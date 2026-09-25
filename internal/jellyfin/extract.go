package jellyfin

import "fmt"

// MediaItemFrom is the compact form of a raw Jellyfin item, as lists show
// it. The overview is truncated to OverviewMaxLen. last_played and date_added
// are set by the callers whose lists are ordered or filtered by them.
func MediaItemFrom(m map[string]any) MediaItem {
	if m == nil {
		return MediaItem{}
	}
	item := MediaItem{
		ID:   GetString(m, "Id"),
		Name: GetString(m, "Name"),
		Type: GetString(m, "Type"),
	}
	if year := GetIntPtr(m, "ProductionYear"); year != nil {
		item.Year = *year
	}
	item.Overview = Truncate(GetString(m, "Overview"), OverviewMaxLen)
	if rating := GetFloat(m, "CommunityRating"); rating > 0 {
		item.CommunityRating = rating
	}
	item.OfficialRating = GetString(m, "OfficialRating")
	if rt := GetInt64(m, "RunTimeTicks"); rt > 0 {
		item.RuntimeMinutes = rt / TicksPerMinute
	}
	if ud := ToMap(m["UserData"]); ud != nil {
		item.Played = GetBool(ud, "Played")
		item.Progress = progressOf(ud)
		item.Favorite = GetBool(ud, "IsFavorite")
	}
	item.SeriesName = GetString(m, "SeriesName")
	if idx := GetInt(m, "IndexNumber"); idx > 0 {
		item.IndexNumber = idx
	}
	if pidx := GetInt(m, "ParentIndexNumber"); pidx > 0 {
		item.ParentIndexNumber = pidx
	}
	// Only Live TV programs and recordings carry a StartDate, and for them
	// StartDate and EndDate are instants. A series' EndDate is a calendar day
	// instead, which is why EndDate is read only beside a StartDate.
	if start := GetString(m, "StartDate"); start != "" {
		item.StartTime = LocalDateTime(start)
		if end := GetString(m, "EndDate"); end != "" {
			item.EndTime = LocalDateTime(end)
		}
		item.ChannelName = GetString(m, "ChannelName")
	}
	return item
}

// MediaItemsFrom is MediaItemFrom over a list of raw items, skipping entries
// that are not objects. It never returns nil.
func MediaItemsFrom(items []any) []MediaItem {
	out := make([]MediaItem, 0, len(items))
	for _, raw := range items {
		if m := ToMap(raw); m != nil {
			out = append(out, MediaItemFrom(m))
		}
	}
	return out
}

// ItemsFrom is MediaItemsFrom over the Items of a query result.
func ItemsFrom(result map[string]any) []MediaItem {
	return MediaItemsFrom(ToSlice(result["Items"]))
}

// progressOf is a user-data PlayedPercentage as a whole percentage, or ""
// when playback is not in progress.
func progressOf(ud map[string]any) string {
	if pct := GetFloat(ud, "PlayedPercentage"); pct > 0 {
		return fmt.Sprintf("%.0f%%", pct)
	}
	return ""
}

// LastPlayedOf is a raw item's LastPlayedDate as Jellyfin reports it, or ""
// when the user never played it.
func LastPlayedOf(m map[string]any) string {
	if ud := ToMap(m["UserData"]); ud != nil {
		return GetString(ud, "LastPlayedDate")
	}
	return ""
}

// DetailedItemFrom is the full form of a raw Jellyfin item, as
// jellyfin_get_item and the item resource show it.
func DetailedItemFrom(m map[string]any) DetailedItemOutput {
	compact := MediaItemFrom(m)
	item := DetailedItemOutput{
		ID:                compact.ID,
		Name:              compact.Name,
		Type:              compact.Type,
		Year:              compact.Year,
		Overview:          GetString(m, "Overview"),
		CommunityRating:   compact.CommunityRating,
		OfficialRating:    compact.OfficialRating,
		RuntimeMinutes:    compact.RuntimeMinutes,
		SeriesName:        compact.SeriesName,
		IndexNumber:       compact.IndexNumber,
		ParentIndexNumber: compact.ParentIndexNumber,
		Played:            compact.Played,
		Progress:          compact.Progress,
		Favorite:          compact.Favorite,
	}

	// Dates
	if dc := GetString(m, "DateCreated"); dc != "" {
		item.DateAdded = LocalDate(dc)
	}
	item.PremiereDate = Truncate(GetString(m, "PremiereDate"), DateOnlyLen)
	item.EndDate = Truncate(GetString(m, "EndDate"), DateOnlyLen)
	item.OriginalLanguage = GetString(m, "OriginalLanguage")

	if cr := GetFloat(m, "CriticRating"); cr > 0 {
		item.CriticRating = cr
	}

	item.Taglines = ToStringSlice(m["Taglines"])
	// The list fields serialize as [] when empty, so that "none" is stated
	// and a metadata update that replaces a list can be judged from them.
	item.Genres = nonNil(ToStringSlice(m["Genres"]))
	item.Tags = nonNil(ToStringSlice(m["Tags"]))
	item.LockedFields = nonNil(ToStringSlice(m["LockedFields"]))
	item.Studios = []string{}
	for _, s := range ToSlice(m["Studios"]) {
		if sm := ToMap(s); sm != nil {
			item.Studios = append(item.Studios, GetString(sm, "Name"))
		}
	}

	people := ToSlice(m["People"])
	if len(people) > MaxPeopleInDetail {
		item.Notes = append(item.Notes, fmt.Sprintf("people lists the first %d of %d cast and crew. To check whether someone else appears in this item, use jellyfin_browse with person set to their name.", MaxPeopleInDetail, len(people)))
	}
	for i, p := range people {
		if i >= MaxPeopleInDetail {
			break
		}
		pm := ToMap(p)
		if pm == nil {
			continue
		}
		item.People = append(item.People, PersonInfo{
			Name: GetString(pm, "Name"),
			Type: GetString(pm, "Type"),
			Role: GetString(pm, "Role"),
		})
	}

	// Chapters, each with its start as a clock time to read and in ticks to
	// seek to.
	for _, c := range ToSlice(m["Chapters"]) {
		cm := ToMap(c)
		if cm == nil {
			continue
		}
		ticks := GetNum[int64](cm, "StartPositionTicks")
		item.Chapters = append(item.Chapters, ChapterInfo{
			Name:       GetString(cm, "Name"),
			Start:      clockTime(ticks),
			StartTicks: ticks,
		})
	}

	// Provider IDs (IMDb, TMDB, TVDB) with direct URLs
	if pids := ToMap(m["ProviderIds"]); pids != nil {
		providerIDs := make(map[string]string)
		for k, v := range pids {
			if s, ok := v.(string); ok && s != "" {
				providerIDs[k] = s
			}
		}
		if len(providerIDs) > 0 {
			item.ProviderIDs = providerIDs
		}
		if links := BuildProviderLinks(providerIDs, item.Type); len(links) > 0 {
			item.ExternalURLs = links
		}
	}

	if ud := ToMap(m["UserData"]); ud != nil {
		userData := &UserDataInfo{
			Played:   GetBool(ud, "Played"),
			Favorite: GetBool(ud, "IsFavorite"),
		}
		if pc := GetInt(ud, "PlayCount"); pc > 0 {
			userData.PlayCount = pc
		}
		if pct := GetFloat(ud, "PlayedPercentage"); pct > 0 {
			userData.PlayedPercentage = pct
		}
		if lp := GetString(ud, "LastPlayedDate"); lp != "" {
			userData.LastPlayed = LocalDate(lp)
		}
		item.UserData = userData
	}

	item.FilePath = GetString(m, "Path")

	for _, src := range ToSlice(m["MediaSources"]) {
		if sm := ToMap(src); sm != nil {
			item.MediaSources = append(item.MediaSources, mediaSourceFrom(sm))
		}
	}

	item.Artists = ToStringSlice(m["Artists"])
	item.Album = GetString(m, "Album")
	item.Status = GetString(m, "Status")
	item.HasSubtitles = GetBool(m, "HasSubtitles")
	item.HasLyrics = GetBool(m, "HasLyrics")
	if cc := GetInt(m, "ChildCount"); cc > 0 {
		item.ChildCount = cc
	}
	if ric := GetInt(m, "RecursiveItemCount"); ric > 0 {
		item.RecursiveItemCount = ric
	}
	return item
}

// mediaSourceFrom is a raw media source with its video, audio, and subtitle
// streams. Bitrate is in kbps and size in megabytes, both base 10.
func mediaSourceFrom(sm map[string]any) MediaSourceInfo {
	source := MediaSourceInfo{
		Container: GetString(sm, "Container"),
		Path:      GetString(sm, "Path"),
	}
	if br := GetInt64(sm, "Bitrate"); br > 0 {
		source.BitrateKbps = br / UnitsPerKilo
	}
	if sz := GetInt64(sm, "Size"); sz > 0 {
		source.SizeMB = sz / BytesPerMB
	}
	for _, st := range ToSlice(sm["MediaStreams"]) {
		stm := ToMap(st)
		if stm == nil {
			continue
		}
		switch GetString(stm, "Type") {
		case "Video":
			source.VideoCodec = GetString(stm, "Codec")
			if w := GetInt(stm, "Width"); w > 0 {
				source.Resolution = fmt.Sprintf("%dx%d", w, GetInt(stm, "Height"))
			}
			source.VideoProfile = GetString(stm, "Profile")
			if bitDepth := GetInt(stm, "BitDepth"); bitDepth > 0 {
				source.VideoBitDepth = bitDepth
			}
			source.VideoRange = GetString(stm, "VideoRange")
			if rangeType := GetString(stm, "VideoRangeType"); rangeType != "Unknown" {
				source.VideoRangeType = rangeType
			}
		case "Audio":
			audio := AudioStreamInfo{
				Index:        GetInt(stm, "Index"),
				Codec:        GetString(stm, "Codec"),
				Language:     GetString(stm, "Language"),
				DisplayTitle: GetString(stm, "DisplayTitle"),
				IsDefault:    GetBool(stm, "IsDefault"),
			}
			if ch := GetInt(stm, "Channels"); ch > 0 {
				audio.Channels = ch
			}
			source.AudioStreams = append(source.AudioStreams, audio)
		case "Subtitle":
			source.SubtitleStreams = append(source.SubtitleStreams, SubtitleStreamInfo{
				Index:        GetInt(stm, "Index"),
				Codec:        GetString(stm, "Codec"),
				Language:     GetString(stm, "Language"),
				DisplayTitle: GetString(stm, "DisplayTitle"),
				IsExternal:   GetBool(stm, "IsExternal"),
				IsDefault:    GetBool(stm, "IsDefault"),
				IsForced:     GetBool(stm, "IsForced"),
			})
		}
	}
	return source
}

// SessionFrom is a raw Jellyfin session with its status: playing when it is
// playing an item, otherwise connected.
func SessionFrom(s map[string]any) SessionInfo {
	session := SessionInfo{
		SessionID:    GetString(s, "Id"),
		User:         GetString(s, "UserName"),
		Client:       GetString(s, "Client"),
		DeviceName:   GetString(s, "DeviceName"),
		LastActivity: LocalDateTime(GetString(s, "LastActivityDate")),
		Status:       "connected",

		SupportsMediaControl: GetBool(ToMap(s["Capabilities"]), "SupportsMediaControl"),
	}
	np := ToMap(s["NowPlayingItem"])
	if np == nil {
		return session
	}
	session.Status = "playing"
	nowPlaying := &NowPlayingInfo{
		ID:   GetString(np, "Id"),
		Name: GetString(np, "Name"),
		Type: GetString(np, "Type"),
	}
	if rt := GetInt64(np, "RunTimeTicks"); rt > 0 {
		nowPlaying.RuntimeMinutes = rt / TicksPerMinute
	}
	session.NowPlaying = nowPlaying
	if ps := ToMap(s["PlayState"]); ps != nil {
		playState := &PlayStateInfo{IsPaused: GetBool(ps, "IsPaused")}
		if pos := GetInt64(ps, "PositionTicks"); pos > 0 {
			playState.PositionSeconds = pos / TicksPerSecond
			playState.PositionTicks = pos
		}
		if vol := GetInt(ps, "VolumeLevel"); vol > 0 {
			playState.Volume = vol
		}
		session.PlayState = playState
	}
	return session
}

// SessionsFrom is SessionFrom over raw sessions. It never returns nil.
func SessionsFrom(sessions []map[string]any) []SessionInfo {
	out := make([]SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, SessionFrom(s))
	}
	return out
}

// LibrariesFrom is the list of raw virtual folders as libraries. A library
// with no collection type is mixed. It never returns nil.
func LibrariesFrom(libs []map[string]any) []LibraryInfo {
	items := make([]LibraryInfo, 0, len(libs))
	for _, lib := range libs {
		ct := GetString(lib, "CollectionType")
		if ct == "" {
			ct = "mixed"
		}
		items = append(items, LibraryInfo{
			Name:           GetString(lib, "Name"),
			CollectionType: ct,
			ItemID:         GetString(lib, "ItemId"),
			Paths:          ToStringSlice(lib["Locations"]),
		})
	}
	return items
}

func ExtractUserSummary(u map[string]any) map[string]any {
	user := map[string]any{
		"id":       GetString(u, "Id"),
		"name":     GetString(u, "Name"),
		"is_admin": false,
	}
	if policy := ToMap(u["Policy"]); policy != nil {
		user["is_admin"] = GetBool(policy, "IsAdministrator")
		if GetBool(policy, "IsDisabled") {
			user["is_disabled"] = true
		}
	}
	if la := GetString(u, "LastActivityDate"); la != "" {
		user["last_activity"] = LocalDateTime(la)
	}
	if ll := GetString(u, "LastLoginDate"); ll != "" {
		user["last_login"] = LocalDateTime(ll)
	}
	return user
}

// ExtractDetailedUser adds the user's policy and configuration to
// ExtractUserSummary. It omits HasPassword, which Jellyfin 12 always reports
// as true, so the field cannot be reported truthfully across the supported
// servers.
func ExtractDetailedUser(u map[string]any) map[string]any {
	user := ExtractUserSummary(u)

	if policy := ToMap(u["Policy"]); policy != nil {
		// Library access
		if GetBool(policy, "EnableAllFolders") {
			user["enable_all_folders"] = true
		} else {
			user["enable_all_folders"] = false
			if ids := ToStringSlice(policy["EnabledFolders"]); len(ids) > 0 {
				user["enabled_folders"] = ids
			}
		}
		// Parental controls (only when set)
		if mr := GetIntPtr(policy, "MaxParentalRating"); mr != nil {
			user["max_parental_rating"] = *mr
		}
		if tl := ToStringSlice(policy["BlockedTags"]); len(tl) > 0 {
			user["blocked_tags"] = tl
		}
		// Notable non-default permissions
		if GetBool(policy, "IsHidden") {
			user["is_hidden"] = true
		}
		if !GetBool(policy, "EnableRemoteAccess") {
			user["remote_access"] = false
		}
		if GetBool(policy, "EnableContentDeletion") {
			user["can_delete_content"] = true
		}
		// Limits (only when non-zero)
		if ms := GetInt(policy, "MaxActiveSessions"); ms > 0 {
			user["max_active_sessions"] = ms
		}
		if bl := GetInt64(policy, "RemoteClientBitrateLimit"); bl > 0 {
			user["remote_bitrate_limit"] = bl
		}
		if la := GetInt(policy, "InvalidLoginAttemptCount"); la > 0 {
			user["invalid_login_attempt_count"] = la
		}
	}

	if config := ToMap(u["Configuration"]); config != nil {
		prefs := make(map[string]any)
		if sl := GetString(config, "SubtitleLanguagePreference"); sl != "" {
			prefs["subtitle_language"] = sl
		}
		if al := GetString(config, "AudioLanguagePreference"); al != "" {
			prefs["audio_language"] = al
		}
		if sm := GetString(config, "SubtitleMode"); sm != "" {
			prefs["subtitle_mode"] = sm
		}
		if !GetBool(config, "PlayDefaultAudioTrack") {
			prefs["play_default_audio"] = false
		}
		if !GetBool(config, "EnableNextEpisodeAutoPlay") {
			prefs["next_episode_auto_play"] = false
		}
		if GetBool(config, "DisplayMissingEpisodes") {
			prefs["display_missing_episodes"] = true
		}
		if len(prefs) > 0 {
			user["preferences"] = prefs
		}
	}

	return user
}

// clockTime formats a position in ticks as hours, minutes, and seconds, such
// as 1:02:03.
func clockTime(ticks int64) string {
	sec := ticks / TicksPerSecond
	return fmt.Sprintf("%d:%02d:%02d", sec/3600, sec/60%60, sec%60)
}

// nonNil returns an empty slice in place of nil, so the field serializes as [].
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
