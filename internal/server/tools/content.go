package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

// metadataProviderKeys are the names of the server's MetadataProvider enum,
// spelled as its provider lookups expect them in a ProviderIds dictionary.
var metadataProviderKeys = []string{
	"Custom", "Imdb", "Tmdb", "Tvdb", "Tvcom", "TmdbCollection",
	"MusicBrainzAlbum", "MusicBrainzAlbumArtist", "MusicBrainzArtist", "MusicBrainzReleaseGroup",
	"Zap2It", "TvRage", "AudioDbArtist", "AudioDbAlbum", "MusicBrainzTrack", "TvMaze", "MusicBrainzRecording",
}

// providerIDKey returns name in the server's spelling when it names a
// built-in metadata provider, and trimmed but otherwise unchanged when it
// names a provider a plugin defines. Jellyfin 12 canonicalizes the key itself
// when it applies a search result, but 10.11 keeps the dictionary it receives
// and looks keys up case-sensitively, so a key in another case goes unused.
func providerIDKey(name string) string {
	name = strings.TrimSpace(name)
	for _, key := range metadataProviderKeys {
		if strings.EqualFold(name, key) {
			return key
		}
	}
	return name
}

// subtitleUploadFormats are the formats the server accepts for an uploaded
// subtitle: its subtitle file extensions without the dot. Jellyfin 10.11.7
// and later reject any other format.
var subtitleUploadFormats = []string{"ass", "mks", "sami", "smi", "srt", "ssa", "sub", "sup", "vtt"}

// uploadImageTypes are the media types an uploaded image may have. The
// server takes the saved file's extension from the request's Content-Type,
// and its image encoder decodes each of these.
var uploadImageTypes = []string{"image/jpeg", "image/png", "image/webp", "image/gif", "image/bmp"}

// imageTypes are Jellyfin's ImageType values. image_type is a path segment of
// the image routes, so only these values are sent.
var imageTypes = []string{"Primary", "Art", "Backdrop", "Banner", "Logo", "Thumb", "Disc", "Box", "Screenshot", "Menu", "Chapter", "BoxRear", "Profile"}

// maxUploadBytes bounds a decoded image or subtitle upload. Jellyfin receives
// either as base64 in a request body, and Kestrel's default request-body limit
// of 30,000,000 bytes, which Jellyfin 10.11 and 12 neither raise nor let an
// administrator change, admits the base64 form of this size. A larger body
// fails with an uninformative 500.
const maxUploadBytes = 20 << 20

// MaxMessageBytes is the largest JSON-RPC message the server reads, on stdio
// and over HTTP. It covers an upload of maxUploadBytes as base64, with room
// for line breaks and JSON escaping, plus the request around it, so an
// oversized upload still arrives and is reported as a tool error. A larger
// message ends a stdio session and gets 413 over HTTP.
const MaxMessageBytes = 32 << 20

// normalizeBase64 strips all whitespace from s and checks that the rest is
// standard, padded base64. It returns the stripped text, which is what the
// server expects to receive, together with the bytes the text encodes.
// decodeBase64Head returns s without whitespace, the number of bytes it
// decodes to, and up to its first 512 decoded bytes, which is what
// http.DetectContentType reads. The whole value is decoded to check it, but
// never held: decoding stops one byte past limit, so an upload costs the
// base64 text the call already carries and nothing more.
func decodeBase64Head(s string, limit int) (text string, size int, head []byte, err error) {
	text = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
	if text == "" {
		return "", 0, nil, errors.New("the data is empty")
	}
	dec := base64.NewDecoder(base64.StdEncoding, strings.NewReader(text))
	head = make([]byte, 512)
	n, err := io.ReadFull(dec, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", 0, nil, err
	}
	head = head[:n]
	rest, err := io.Copy(io.Discard, io.LimitReader(dec, int64(limit-n)+1))
	if err != nil {
		return "", 0, nil, err
	}
	return text, n + int(rest), head, nil
}

func formatStringSlice(vals []string) string {
	quoted := make([]string, len(vals))
	for i, v := range vals {
		quoted[i] = fmt.Sprintf("%q", v)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// metadataChangeSummary formats the fields and values a metadata update or
// batch_update writes, keeping field names in their input spelling.
func metadataChangeSummary(args jf.MetadataInput) []string {
	var fields []string
	if args.Name != "" {
		fields = append(fields, fmt.Sprintf("name=%q", args.Name))
	}
	if args.Overview != "" {
		ov := args.Overview
		if len([]rune(ov)) > 60 {
			ov = jf.Truncate(ov, 60) + "…"
		}
		fields = append(fields, fmt.Sprintf("overview=%q", ov))
	}
	if len(args.Genres) > 0 {
		fields = append(fields, fmt.Sprintf("genres=%s", formatStringSlice(args.Genres)))
	}
	if len(args.Tags) > 0 {
		fields = append(fields, fmt.Sprintf("tags=%s", formatStringSlice(args.Tags)))
	}
	if len(args.Studios) > 0 {
		fields = append(fields, fmt.Sprintf("studios=%s", formatStringSlice(args.Studios)))
	}
	if args.Year != nil {
		fields = append(fields, fmt.Sprintf("production_year=%d", *args.Year))
	}
	if args.CommunityRating != nil {
		fields = append(fields, fmt.Sprintf("community_rating=%.1f", *args.CommunityRating))
	}
	if args.OfficialRating != "" {
		fields = append(fields, fmt.Sprintf("official_rating=%q", args.OfficialRating))
	}
	if args.SortName != "" {
		fields = append(fields, fmt.Sprintf("sort_name=%q", args.SortName))
	}
	if len(args.LockedFields) > 0 {
		fields = append(fields, fmt.Sprintf("locked_fields=%s", formatStringSlice(args.LockedFields)))
	}
	return fields
}

func metadataListReplacementNote(args jf.MetadataInput) string {
	var lists []string
	if len(args.Genres) > 0 {
		lists = append(lists, "genres")
	}
	if len(args.Tags) > 0 {
		lists = append(lists, "tags")
	}
	if len(args.Studios) > 0 {
		lists = append(lists, "studios")
	}
	if len(args.LockedFields) > 0 {
		lists = append(lists, "locked_fields")
	}
	if len(lists) == 0 {
		return ""
	}
	return fmt.Sprintf(" Replacing %s overwrites existing values in %s.", strings.Join(lists, ", "), plural(len(lists), "that list", "those lists"))
}

func RegisterContentTools(server *mcp.Server, client jf.Client, enabled func(string, *mcp.ToolAnnotations) bool) {

	// --- jellyfin_metadata ---
	if ok, ro, annot := toolMode(enabled, "jellyfin_metadata", AnnotDestructiveOpenWorld, AnnotReadOnlyOpenWorld); ok {
		roActions := []any{"search", "editor_info", "external_ids"}
		schema := jf.WithEnums[jf.MetadataInput](map[string][]any{
			"action": {"search", "apply", "update", "batch_update", "editor_info", "external_ids", "merge_versions", "split_versions"},
		})
		desc := "Search online metadata providers, identify items (apply), edit item fields (update/batch_update), inspect editor_info/external_ids, or merge/split multi-version video entries (omit confirm first to preview changes)."
		if ro {
			schema = jf.WithEnumsFiltered[jf.MetadataInput](map[string][]any{"action": roActions}, []string{"action", "item_id", "search_type", "search_query", "search_year"})
			desc = "Search online metadata providers or inspect item metadata editor info and external provider IDs."
		}
		mcp.AddTool(server, &mcp.Tool{
			Name:        "jellyfin_metadata",
			Title:       "Metadata & Versions",
			InputSchema: schema,
			Description: desc,
			Annotations: annot,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.MetadataInput) (*mcp.CallToolResult, any, error) {
			if ro {
				if res := requireReadOnlyAction(args.Action, roActions); res != nil {
					return res, nil, nil
				}
			}
			switch args.Action {
			case "search":
				if args.SearchType == "" || args.SearchQuery == "" {
					return jf.ErrResult("search_type and search_query are required. search_type options: Movie, Series, Person, Book, BoxSet, MusicAlbum, MusicArtist"), nil, nil
				}
				body := map[string]any{
					"SearchInfo": map[string]any{
						"Name": args.SearchQuery,
					},
				}
				if args.SearchYear != nil {
					body["SearchInfo"].(map[string]any)["Year"] = *args.SearchYear
				}
				endpoint := fmt.Sprintf("/Items/RemoteSearch/%s", jf.SanitizeID(args.SearchType))
				var results []map[string]any
				if err := client.Post(ctx, endpoint, nil, body, &results); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := make([]map[string]any, 0, len(results))
				for _, r := range results {
					item := map[string]any{
						"name": jf.GetString(r, "Name"),
					}
					if year := jf.GetIntPtr(r, "ProductionYear"); year != nil {
						item["year"] = *year
					}
					if pids := jf.ToMap(r["ProviderIds"]); pids != nil {
						item["provider_ids"] = pids
					}
					if ov := jf.GetString(r, "Overview"); ov != "" {
						item["overview"] = jf.Truncate(ov, jf.OverviewMaxLen)
					}
					items = append(items, item)
				}
				return jf.TextResult(fmt.Sprintf("Found %d metadata results:\n\n%s", len(items), jf.FormatJSON(items))), nil, nil

			case "apply":
				providerKey := providerIDKey(args.ProviderName)
				providerID := strings.TrimSpace(args.ProviderID)
				if args.ItemID == "" || providerKey == "" || providerID == "" {
					return jf.ErrResult("item_id, provider_name, and provider_id are required. Use 'search' first to find provider IDs."), nil, nil
				}
				// The body is the chosen RemoteSearchResult itself. The server
				// replaces the item's provider IDs with its ProviderIds and seeds
				// the refresh with its Name and ProductionYear; with only a
				// provider ID sent, the providers resolve the match by that ID.
				body := map[string]any{
					"ProviderIds": map[string]string{providerKey: providerID},
				}
				// ReplaceAllImages matches the server's and jellyfin-web's
				// identify default: identifying re-points the item at a
				// different title, so the old match's artwork is replaced along
				// with its metadata.
				params := url.Values{"ReplaceAllImages": {"true"}}
				var applyWarning string
				if jf.ConfirmationNeededIn(ctx, req, args.Confirm) {
					name, errRes := requireItemName(ctx, client, args.ItemID, "")
					if errRes != nil {
						return errRes, nil, nil
					}
					applyWarning = fmt.Sprintf("Identify '%s' using %s ID '%s'? Its provider IDs, metadata, and images are replaced with the provider match.", name, providerKey, providerID)
				}
				if result := jf.ConfirmationGate(ctx, req, args.Confirm, applyWarning); result != nil {
					return result, nil, nil
				}
				endpoint := fmt.Sprintf("/Items/RemoteSearch/Apply/%s", jf.SanitizeID(args.ItemID))
				if err := client.PostNoContent(ctx, endpoint, params, body); err != nil {
					return jf.ErrResult("Failed to apply metadata: %v", err), nil, nil
				}
				return jf.TextResult("Metadata applied. The provider's data replaces the item's as the refresh runs."), nil, nil

			case "update":
				if args.ItemID == "" {
					return jf.ErrResult("item_id is required for update."), nil, nil
				}
				fields := metadataChangeSummary(args)
				if len(fields) == 0 {
					return jf.ErrResult("No fields to update. Provide at least one field (name, overview, genres, tags, studios, production_year, community_rating, official_rating, sort_name, locked_fields)."), nil, nil
				}
				userID, err := client.GetUserID(ctx)
				if err != nil {
					return jf.ErrResult("Jellyfin error: %v", err), nil, nil
				}
				listNote := metadataListReplacementNote(args)
				if args.DryRun != nil && *args.DryRun {
					if args.Confirm != nil && *args.Confirm {
						return jf.ErrResult("confirm=true cannot be combined with dry_run=true; omit dry_run or pass both dry_run=false and confirm=true to apply a confirmed change."), nil, nil
					}
					name, errRes := requireItemName(ctx, client, args.ItemID, userID)
					if errRes != nil {
						return errRes, nil, nil
					}
					return jf.TextResult(fmt.Sprintf("Dry run, no changes made. An update of '%s' writes: %s.%s\n\nCall again without dry_run to apply; the user is asked to confirm.", name, strings.Join(fields, ", "), listNote)), nil, nil
				}
				var updateWarning string
				if jf.ConfirmationNeededIn(ctx, req, args.Confirm) {
					name, errRes := requireItemName(ctx, client, args.ItemID, userID)
					if errRes != nil {
						return errRes, nil, nil
					}
					updateWarning = fmt.Sprintf("Update metadata of '%s'? Changes: %s.%s", name, strings.Join(fields, ", "), listNote)
				}
				if result := jf.ConfirmationGate(ctx, req, args.Confirm, updateWarning); result != nil {
					return result, nil, nil
				}

				jf.ReportProgress(ctx, req, 0, 2, "Fetching current item data...")

				var current map[string]any
				if err := client.Get(ctx, fmt.Sprintf("/Items/%s", jf.SanitizeID(args.ItemID)), url.Values{"UserId": {userID}}, &current); err != nil {
					return jf.ErrResult("Failed to get item: %v", err), nil, nil
				}

				jf.ReportProgress(ctx, req, 1, 2, "Applying updates...")

				jf.ApplyMetadataFields(current, args)
				endpoint := fmt.Sprintf("/Items/%s", jf.SanitizeID(args.ItemID))
				if err := client.PostNoContent(ctx, endpoint, nil, current); err != nil {
					return jf.ErrResult("Failed to update item: %v", err), nil, nil
				}
				return jf.TextResult("Item metadata updated."), nil, nil

			case "batch_update":
				args.ItemIDs = jf.SplitIDs(args.ItemIDs)
				if len(args.ItemIDs) == 0 {
					return jf.ErrResult("item_ids is required for batch_update (max 50)."), nil, nil
				}
				if len(args.ItemIDs) > 50 {
					return jf.ErrResult("batch_update supports at most 50 items at a time."), nil, nil
				}
				fields := metadataChangeSummary(args)
				if len(fields) == 0 {
					return jf.ErrResult("No fields to update. Provide at least one field (name, overview, genres, tags, studios, production_year, community_rating, official_rating, sort_name, locked_fields)."), nil, nil
				}
				userID, err := client.GetUserID(ctx)
				if err != nil {
					return jf.ErrResult("Jellyfin error: %v", err), nil, nil
				}
				listNote := metadataListReplacementNote(args)

				// Dry-run mode: default true — preview changes without applying
				dryRun := args.DryRun == nil || *args.DryRun
				if dryRun {
					if args.Confirm != nil && *args.Confirm {
						if args.DryRun == nil {
							return jf.ErrResult("dry_run defaults to true for 'batch_update'. Pass both dry_run=false and confirm=true to apply a confirmed change (or dry_run=false without confirm to preview confirmation first)."), nil, nil
						}
						return jf.ErrResult("confirm=true cannot be combined with dry_run=true; pass both dry_run=false and confirm=true to apply a confirmed change."), nil, nil
					}
					names, errRes := requireItemNames(ctx, client, args.ItemIDs, userID)
					if errRes != nil {
						return errRes, nil, nil
					}
					targetNames := nameList(args.ItemIDs, names)
					return jf.TextResult(fmt.Sprintf("Dry run, no changes made. An update of %d %s (%s) writes: %s.%s\n\nSet dry_run=false to apply; the user is asked to confirm.", len(args.ItemIDs), plural(len(args.ItemIDs), "item", "items"), targetNames, strings.Join(fields, ", "), listNote)), nil, nil
				}

				var batchWarning string
				if jf.ConfirmationNeededIn(ctx, req, args.Confirm) {
					names, errRes := requireItemNames(ctx, client, args.ItemIDs, userID)
					if errRes != nil {
						return errRes, nil, nil
					}
					targetNames := nameList(args.ItemIDs, names)
					batchWarning = fmt.Sprintf("Update metadata of %d %s (%s)? Changes: %s.%s", len(args.ItemIDs), plural(len(args.ItemIDs), "item", "items"), targetNames, strings.Join(fields, ", "), listNote)
				}
				if result := jf.ConfirmationGate(ctx, req, args.Confirm, batchWarning); result != nil {
					return result, nil, nil
				}
				total := float64(len(args.ItemIDs))
				successes := 0
				failures := 0
				for i, id := range args.ItemIDs {
					jf.ReportProgress(ctx, req, float64(i), total, fmt.Sprintf("Updating item %d/%d", i+1, int(total)))
					var current map[string]any
					if err := client.Get(ctx, fmt.Sprintf("/Items/%s", jf.SanitizeID(id)), url.Values{"UserId": {userID}}, &current); err != nil {
						failures++
						continue
					}
					jf.ApplyMetadataFields(current, args)
					if err := client.PostNoContent(ctx, fmt.Sprintf("/Items/%s", jf.SanitizeID(id)), nil, current); err != nil {
						failures++
						continue
					}
					successes++
				}
				jf.ReportProgress(ctx, req, total, total, "Batch update complete")
				return jf.TextResult(fmt.Sprintf("Batch update complete. Updated %d of %d %s. %d %s.", successes, len(args.ItemIDs), plural(len(args.ItemIDs), "item", "items"), failures, plural(failures, "failure", "failures"))), nil, nil

			case "editor_info":
				if args.ItemID == "" {
					return jf.ErrResult("item_id is required."), nil, nil
				}
				var info map[string]any
				endpoint := fmt.Sprintf("/Items/%s/MetadataEditor", jf.SanitizeID(args.ItemID))
				if err := client.Get(ctx, endpoint, nil, &info); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				return jf.TextResult(jf.FormatJSON(info)), nil, nil

			case "external_ids":
				if args.ItemID == "" {
					return jf.ErrResult("item_id is required."), nil, nil
				}
				var ids []map[string]any
				endpoint := fmt.Sprintf("/Items/%s/ExternalIdInfos", jf.SanitizeID(args.ItemID))
				if err := client.Get(ctx, endpoint, nil, &ids); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				return jf.TextResult(jf.FormatJSON(ids)), nil, nil

			case "merge_versions":
				ids := jf.SplitIDs(args.ItemIDs)
				if len(ids) < 2 {
					return jf.ErrResult("item_ids must contain at least 2 item IDs to merge."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				var warning string
				if jf.ConfirmationNeededIn(ctx, req, args.Confirm) {
					var preErr *mcp.CallToolResult
					warning, preErr = inspectMergeVersions(ctx, client, ids)
					if preErr != nil {
						return preErr, nil, nil
					}
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, warning); result != nil {
					return result, nil, nil
				}
				params := url.Values{"ids": {jf.JoinIDs(ids)}}
				if err := client.PostNoContent(ctx, "/Videos/MergeVersions", params, nil); err != nil {
					return jf.ErrResult("Failed to merge versions: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Merged %d items as alternate versions.", len(ids))), nil, nil

			case "split_versions":
				if args.ItemID == "" {
					return jf.ErrResultWithHint("Use jellyfin_search to find the merged item ID.", "item_id is required for split_versions."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				warning, preErr := inspectSplitVersions(ctx, client, args.ItemID)
				if preErr != nil {
					return preErr, nil, nil
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, warning); result != nil {
					return result, nil, nil
				}
				endpoint := fmt.Sprintf("/Videos/%s/AlternateSources", jf.SanitizeID(args.ItemID))
				if err := client.Del(ctx, endpoint, nil); err != nil {
					return jf.ErrResult("Failed to split versions: %v", err), nil, nil
				}
				return jf.TextResult("Alternate versions split into separate items."), nil, nil

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: search, apply, update, batch_update, editor_info, external_ids, merge_versions, split_versions", args.Action), nil, nil
			}
		})
	}

	// --- jellyfin_subtitles_lyrics ---
	if ok, ro, annot := toolMode(enabled, "jellyfin_subtitles_lyrics", AnnotDestructiveOpenWorld, AnnotReadOnlyOpenWorld); ok {
		roActions := []any{"search_subtitles", "get_lyrics", "search_lyrics"}
		schema := jf.WithEnums[jf.SubtitlesLyricsInput](map[string][]any{
			"action": {"search_subtitles", "download_subtitle", "delete_subtitle", "batch_download_subtitles", "upload_subtitle", "get_lyrics", "search_lyrics", "download_lyrics", "delete_lyrics"},
		})
		desc := fmt.Sprintf("Search, download, upload (formats: ass, mks, sami, smi, srt, ssa, sub, sup, vtt; max %d MiB), or delete external subtitles and song lyrics.", maxUploadBytes>>20)
		if ro {
			schema = jf.WithEnumsFiltered[jf.SubtitlesLyricsInput](map[string][]any{"action": roActions}, []string{"action", "item_id", "language"})
			desc = "Search online subtitle/lyric providers or read an item's current lyrics."
		}
		mcp.AddTool(server, &mcp.Tool{
			Name:        "jellyfin_subtitles_lyrics",
			Title:       "Subtitles & Lyrics",
			InputSchema: schema,
			Description: desc,
			Annotations: annot,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.SubtitlesLyricsInput) (*mcp.CallToolResult, any, error) {
			if ro {
				if res := requireReadOnlyAction(args.Action, roActions); res != nil {
					return res, nil, nil
				}
			}
			itemID := jf.SanitizeID(strings.TrimSpace(args.ItemID))

			switch args.Action {
			case "search_subtitles":
				if itemID == "" {
					return jf.ErrResult("item_id is required for search_subtitles."), nil, nil
				}
				lang := args.Language
				if lang == "" {
					lang = "en"
				}
				endpoint := fmt.Sprintf("/Items/%s/RemoteSearch/Subtitles/%s", itemID, jf.SanitizeID(lang))
				var results []map[string]any
				if err := client.Get(ctx, endpoint, nil, &results); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := make([]map[string]any, 0, len(results))
				for _, r := range results {
					items = append(items, map[string]any{
						"id":             jf.GetString(r, "Id"),
						"name":           jf.GetString(r, "Name"),
						"provider":       jf.GetString(r, "ProviderName"),
						"format":         jf.GetString(r, "Format"),
						"language":       jf.GetString(r, "ThreeLetterISOLanguageName"),
						"download_count": jf.GetInt(r, "DownloadCount"),
					})
				}
				return jf.TextResult(fmt.Sprintf("Found %d subtitles:\n\n%s", len(items), jf.FormatJSON(items))), nil, nil

			case "download_subtitle":
				if args.SubtitleID == "" {
					return jf.ErrResult("subtitle_id is required. Use 'search_subtitles' to find subtitle IDs."), nil, nil
				}
				if itemID == "" {
					return jf.ErrResult("item_id is required for download_subtitle."), nil, nil
				}
				endpoint := fmt.Sprintf("/Items/%s/RemoteSearch/Subtitles/%s", itemID, jf.SanitizeID(args.SubtitleID))
				if err := client.PostNoContent(ctx, endpoint, nil, nil); err != nil {
					return jf.ErrResult("Failed to download subtitle: %v", err), nil, nil
				}
				return jf.TextResult("Subtitle downloaded and added to item."), nil, nil

			case "delete_subtitle":
				if args.SubtitleIndex == nil {
					return jf.ErrResult("subtitle_index is required. Get the index from jellyfin_get_item media source info."), nil, nil
				}
				if itemID == "" {
					return jf.ErrResult("item_id is required for delete_subtitle."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				warning, preErr := inspectSubtitleDeletion(ctx, client, args.ItemID, *args.SubtitleIndex)
				if preErr != nil {
					return preErr, nil, nil
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, warning); result != nil {
					return result, nil, nil
				}
				endpoint := fmt.Sprintf("/Videos/%s/Subtitles/%d", itemID, *args.SubtitleIndex)
				if err := client.Del(ctx, endpoint, nil); err != nil {
					return jf.ErrResult("Failed to delete subtitle: %v", err), nil, nil
				}
				return jf.TextResult("Subtitle track deleted."), nil, nil

			case "batch_download_subtitles":
				args.ItemIDs = jf.SplitIDs(args.ItemIDs)
				if len(args.ItemIDs) == 0 {
					return jf.ErrResult("item_ids is required for batch_download_subtitles (max 25)."), nil, nil
				}
				if len(args.ItemIDs) > 25 {
					return jf.ErrResult("batch_download_subtitles supports at most 25 items at a time."), nil, nil
				}
				lang := args.Language
				if lang == "" {
					lang = "en"
				}
				var subWarning string
				if jf.ConfirmationNeededIn(ctx, req, args.Confirm) {
					userID, _ := client.GetUserID(ctx)
					names, errRes := requireItemNames(ctx, client, args.ItemIDs, userID)
					if errRes != nil {
						return errRes, nil, nil
					}
					subWarning = fmt.Sprintf("Search for and download '%s' subtitles for %d %s (%s)? The first provider match for each item is saved.", lang, len(args.ItemIDs), plural(len(args.ItemIDs), "item", "items"), nameList(args.ItemIDs, names))
				}
				if result := jf.ConfirmationGate(ctx, req, args.Confirm, subWarning); result != nil {
					return result, nil, nil
				}
				total := float64(len(args.ItemIDs))
				var results []map[string]any
				for i, id := range args.ItemIDs {
					jf.ReportProgress(ctx, req, float64(i), total, fmt.Sprintf("Processing item %d/%d", i+1, int(total)))
					escapedID := jf.SanitizeID(id)
					endpoint := fmt.Sprintf("/Items/%s/RemoteSearch/Subtitles/%s", escapedID, jf.SanitizeID(lang))
					var searchResults []map[string]any
					if err := client.Get(ctx, endpoint, nil, &searchResults); err != nil {
						results = append(results, map[string]any{"item_id": id, "status": "search_failed", "error": err.Error()})
						continue
					}
					if len(searchResults) == 0 {
						results = append(results, map[string]any{"item_id": id, "status": "no_results"})
						continue
					}
					// Pick best result (first)
					best := searchResults[0]
					subID := jf.GetString(best, "Id")
					dlEndpoint := fmt.Sprintf("/Items/%s/RemoteSearch/Subtitles/%s", escapedID, jf.SanitizeID(subID))
					if err := client.PostNoContent(ctx, dlEndpoint, nil, nil); err != nil {
						results = append(results, map[string]any{"item_id": id, "status": "download_failed", "subtitle": jf.GetString(best, "Name"), "error": err.Error()})
						continue
					}
					results = append(results, map[string]any{"item_id": id, "status": "downloaded", "subtitle": jf.GetString(best, "Name"), "provider": jf.GetString(best, "ProviderName")})
				}
				jf.ReportProgress(ctx, req, total, total, "Batch subtitle download complete")
				downloaded := 0
				for _, r := range results {
					if jf.GetString(r, "status") == "downloaded" {
						downloaded++
					}
				}
				return jf.TextResult(fmt.Sprintf("Batch subtitle download complete. Downloaded %d of %d:\n\n%s", downloaded, len(args.ItemIDs), jf.FormatJSON(results))), nil, nil

			case "get_lyrics":
				if itemID == "" {
					return jf.ErrResult("item_id is required for get_lyrics."), nil, nil
				}
				endpoint := fmt.Sprintf("/Audio/%s/Lyrics", itemID)
				var result map[string]any
				if err := client.Get(ctx, endpoint, nil, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v. This item may not have lyrics.", err), nil, nil
				}
				return jf.TextResult(jf.FormatJSON(result)), nil, nil

			case "search_lyrics":
				if itemID == "" {
					return jf.ErrResult("item_id is required for search_lyrics."), nil, nil
				}
				endpoint := fmt.Sprintf("/Audio/%s/RemoteSearch/Lyrics", itemID)
				var results []map[string]any
				if err := client.Get(ctx, endpoint, nil, &results); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Found %d lyric results:\n\n%s", len(results), jf.FormatJSON(results))), nil, nil

			case "download_lyrics":
				if args.LyricID == "" {
					return jf.ErrResult("lyric_id is required. Use 'search_lyrics' to find lyric IDs."), nil, nil
				}
				if itemID == "" {
					return jf.ErrResult("item_id is required for download_lyrics."), nil, nil
				}
				endpoint := fmt.Sprintf("/Audio/%s/RemoteSearch/Lyrics/%s", itemID, jf.SanitizeID(args.LyricID))
				if err := client.PostNoContent(ctx, endpoint, nil, nil); err != nil {
					return jf.ErrResult("Failed to download lyrics: %v", err), nil, nil
				}
				return jf.TextResult("Lyrics downloaded and added to item."), nil, nil

			case "upload_subtitle":
				if itemID == "" {
					return jf.ErrResult("item_id is required for upload_subtitle."), nil, nil
				}
				if args.SubtitleData == "" || args.SubtitleFormat == "" {
					return jf.ErrResult("subtitle_data (base64-encoded) and subtitle_format (such as srt, ass, vtt) are required for upload_subtitle."), nil, nil
				}
				format := strings.ToLower(strings.TrimSpace(args.SubtitleFormat))
				if !slices.Contains(subtitleUploadFormats, format) {
					return jf.ErrResult("Unsupported subtitle_format '%s'. Jellyfin accepts: %s.", args.SubtitleFormat, strings.Join(subtitleUploadFormats, ", ")), nil, nil
				}
				data, size, _, err := decodeBase64Head(args.SubtitleData, maxUploadBytes)
				if err != nil {
					return jf.ErrResult("subtitle_data is not valid base64: %v. Send the standard base64 encoding of the subtitle file.", err), nil, nil
				}
				if size > maxUploadBytes {
					return jf.ErrResult("subtitle_data decodes to %d bytes, which exceeds the upload limit of %d MiB.", size, maxUploadBytes>>20), nil, nil
				}
				lang := args.SubtitleLanguage
				if lang == "" {
					lang = "eng"
				}
				// IsForced and IsHearingImpaired are required members of the
				// server's upload body, so both are always sent. The base64
				// text is spliced into the JSON body as it is, because the
				// standard alphabet holds nothing JSON escapes, so the body is
				// streamed without a second copy of the data.
				fields, err := json.Marshal(map[string]any{
					"Language":          lang,
					"Format":            format,
					"IsForced":          args.IsForced != nil && *args.IsForced,
					"IsHearingImpaired": args.IsHearingImpaired != nil && *args.IsHearingImpaired,
				})
				if err != nil {
					return jf.ErrResult("Failed to build the upload: %v", err), nil, nil
				}
				prefix, suffix := `{"Data":"`, `",`+string(fields[1:])
				body := io.MultiReader(strings.NewReader(prefix), strings.NewReader(data), strings.NewReader(suffix))
				before, countErr := externalSubtitleCount(ctx, client, itemID)
				endpoint := fmt.Sprintf("/Videos/%s/Subtitles", itemID)
				if err := client.PostRaw(ctx, endpoint, nil, body, int64(len(prefix)+len(data)+len(suffix)), "application/json"); err != nil {
					return jf.ErrResult("Failed to upload subtitle: %v", err), nil, nil
				}
				if countErr != nil {
					return jf.TextResult(fmt.Sprintf("Subtitle uploaded (%s, %s).", format, lang)), nil, nil
				}
				// The server lists the new track once it has re-probed the
				// item, which takes a moment; the result reports what it found.
				for range 10 {
					if after, err := externalSubtitleCount(ctx, client, itemID); err == nil && after > before {
						return jf.TextResult(fmt.Sprintf("Subtitle uploaded (%s, %s). The item now has %d external subtitle %s.", format, lang, after, plural(after, "track", "tracks"))), nil, nil
					}
					select {
					case <-ctx.Done():
						return jf.TextResult(fmt.Sprintf("Subtitle uploaded (%s, %s).", format, lang)), nil, nil
					case <-time.After(500 * time.Millisecond):
					}
				}
				return jf.TextResult(fmt.Sprintf("Subtitle uploaded (%s, %s). The track appears once the server finishes refreshing the item.", format, lang)), nil, nil

			case "delete_lyrics":
				if itemID == "" {
					return jf.ErrResult("item_id is required for delete_lyrics."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				var lyricWarning string
				if jf.ConfirmationNeededIn(ctx, req, args.Confirm) {
					name, errRes := requireItemName(ctx, client, args.ItemID, "")
					if errRes != nil {
						return errRes, nil, nil
					}
					lyricWarning = fmt.Sprintf("Delete the lyrics of '%s'? The lyrics file is removed.", name)
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, lyricWarning); result != nil {
					return result, nil, nil
				}
				endpoint := fmt.Sprintf("/Audio/%s/Lyrics", itemID)
				if err := client.Del(ctx, endpoint, nil); err != nil {
					return jf.ErrResult("Failed to delete lyrics: %v", err), nil, nil
				}
				return jf.TextResult("Lyrics deleted."), nil, nil

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: search_subtitles, download_subtitle, delete_subtitle, batch_download_subtitles, upload_subtitle, get_lyrics, search_lyrics, download_lyrics, delete_lyrics", args.Action), nil, nil
			}
		})
	}

	// --- jellyfin_images ---
	if ok, ro, annot := toolMode(enabled, "jellyfin_images", AnnotWriteOpenWorld, AnnotReadOnlyOpenWorld); ok {
		roActions := []any{"list", "get_url", "remote_list"}
		schema := jf.WithEnums[jf.ImagesInput](map[string][]any{
			"action": {"list", "get_url", "remote_list", "remote_download", "upload"},
		})
		desc := fmt.Sprintf("List item artwork, build direct image URLs, browse/download remote provider artwork, or upload base64 images (max %d MiB).", maxUploadBytes>>20)
		if ro {
			schema = jf.WithEnumsFiltered[jf.ImagesInput](map[string][]any{"action": roActions}, []string{"action", "item_id", "image_type", "provider_name", "image_index"})
			desc = "List item artwork, build direct image URLs, or browse remote provider artwork."
		}
		mcp.AddTool(server, &mcp.Tool{
			Name:        "jellyfin_images",
			Title:       "Images",
			InputSchema: schema,
			Description: desc,
			Annotations: annot,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.ImagesInput) (*mcp.CallToolResult, any, error) {
			if ro {
				if res := requireReadOnlyAction(args.Action, roActions); res != nil {
					return res, nil, nil
				}
			}
			itemID := jf.SanitizeID(strings.TrimSpace(args.ItemID))
			imageType := args.ImageType
			if imageType == "" {
				imageType = "Primary"
			}
			if !slices.Contains(imageTypes, imageType) {
				return jf.ErrResult("image_type %q is not a Jellyfin image type. Valid types: %s.", args.ImageType, strings.Join(imageTypes, ", ")), nil, nil
			}
			if slices.Contains([]string{"list", "get_url", "remote_list", "remote_download", "upload"}, args.Action) && itemID == "" {
				return jf.ErrResult("item_id is required for %s.", args.Action), nil, nil
			}

			switch args.Action {
			case "list":
				var images []map[string]any
				endpoint := fmt.Sprintf("/Items/%s/Images", itemID)
				if err := client.Get(ctx, endpoint, nil, &images); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				items := make([]map[string]any, 0, len(images))
				for _, img := range images {
					items = append(items, map[string]any{
						"image_type":  jf.GetString(img, "ImageType"),
						"image_index": jf.GetInt(img, "ImageIndex"),
						"width":       jf.GetInt(img, "Width"),
						"height":      jf.GetInt(img, "Height"),
					})
				}
				return jf.TextResult(fmt.Sprintf("Images (%d):\n\n%s", len(items), jf.FormatJSON(items))), nil, nil

			case "get_url":
				idx := 0
				if args.ImageIndex != nil {
					idx = *args.ImageIndex
				}
				imageURL := fmt.Sprintf("%s/Items/%s/Images/%s/%d", client.BaseURL(), itemID, imageType, idx)
				return jf.TextResult(fmt.Sprintf("Image URL: %s", imageURL)), nil, nil

			case "remote_list":
				params := url.Values{}
				if args.Provider != "" {
					params.Set("ProviderName", args.Provider)
				}
				params.Set("Type", imageType)
				endpoint := fmt.Sprintf("/Items/%s/RemoteImages", itemID)
				var result map[string]any
				if err := client.Get(ctx, endpoint, params, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				rawImages := jf.ToSlice(result["Images"])
				items := make([]map[string]any, 0, len(rawImages))
				for _, raw := range rawImages {
					m := jf.ToMap(raw)
					items = append(items, map[string]any{
						"url":              jf.GetString(m, "Url"),
						"provider_name":    jf.GetString(m, "ProviderName"),
						"type":             jf.GetString(m, "Type"),
						"width":            jf.GetInt(m, "Width"),
						"height":           jf.GetInt(m, "Height"),
						"community_rating": jf.GetFloat(m, "CommunityRating"),
						"language":         jf.GetString(m, "Language"),
					})
				}
				return jf.TextResult(fmt.Sprintf("Remote images (%d):\n\n%s", len(items), jf.FormatJSON(items))), nil, nil

			case "remote_download":
				if args.ImageURL == "" {
					return jf.ErrResult("image_url is required. Use 'remote_list' to find image URLs."), nil, nil
				}
				// The endpoint reads the image type and URL from the query and
				// takes no body.
				params := url.Values{"Type": {imageType}, "ImageUrl": {args.ImageURL}}
				endpoint := fmt.Sprintf("/Items/%s/RemoteImages/Download", itemID)
				if err := client.PostNoContent(ctx, endpoint, params, nil); err != nil {
					return jf.ErrResult("Failed to download image: %v", err), nil, nil
				}
				return jf.TextResult("Image downloaded and set for item."), nil, nil

			case "upload":
				if args.ImageData == "" {
					return jf.ErrResult("image_data (a base64-encoded JPEG, PNG, WebP, GIF, or BMP file) is required for upload."), nil, nil
				}
				// The server base64-decodes the request body itself, so the
				// base64 text is sent. Decoding it here rejects malformed input
				// and identifies the format, which sets the Content-Type.
				data, size, head, err := decodeBase64Head(args.ImageData, maxUploadBytes)
				if err != nil {
					return jf.ErrResult("image_data is not valid base64: %v. Send the standard base64 encoding of the image file, without a data: URL prefix.", err), nil, nil
				}
				if size > maxUploadBytes {
					return jf.ErrResult("image_data decodes to %d bytes, which exceeds the upload limit of %d MiB.", size, maxUploadBytes>>20), nil, nil
				}
				contentType := http.DetectContentType(head)
				if !slices.Contains(uploadImageTypes, contentType) {
					return jf.ErrResult("image_data is not a JPEG, PNG, WebP, GIF, or BMP image."), nil, nil
				}
				endpoint := fmt.Sprintf("/Items/%s/Images/%s", itemID, imageType)
				if err := client.PostRaw(ctx, endpoint, nil, strings.NewReader(data), int64(len(data)), contentType); err != nil {
					return jf.ErrResult("Failed to upload image: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Image uploaded as %s for item.", imageType)), nil, nil

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: list, get_url, remote_list, remote_download, upload", args.Action), nil, nil
			}
		})
	}
}

// externalSubtitleCount counts the item's external subtitle tracks as the
// configured user sees them.
func externalSubtitleCount(ctx context.Context, client jf.Client, itemID string) (int, error) {
	userID, err := client.GetUserID(ctx)
	if err != nil {
		return 0, err
	}
	var item map[string]any
	if err := client.Get(ctx, "/Items/"+jf.SanitizeID(itemID), url.Values{"UserId": {userID}}, &item); err != nil {
		return 0, err
	}
	n := 0
	for _, raw := range jf.ToSlice(item["MediaStreams"]) {
		if m := jf.ToMap(raw); jf.GetString(m, "Type") == "Subtitle" && jf.GetBool(m, "IsExternal") {
			n++
		}
	}
	return n, nil
}

// inspectMergeVersions validates requested item IDs when possible and builds
// the merge_versions confirmation prompt with item names and resolutions.
func inspectMergeVersions(ctx context.Context, client jf.Client, ids []string) (string, *mcp.CallToolResult) {
	labels := make(map[string]string, len(ids))
	userID, _ := client.GetUserID(ctx)
	params := url.Values{"Ids": {jf.JoinIDs(ids)}, "Fields": {"MediaStreams"}}
	if userID != "" {
		params.Set("UserId", userID)
	}
	var out struct {
		Items []map[string]any `json:"Items"`
	}
	if err := client.Get(ctx, "/Items", params, &out); err != nil {
		var apiErr *jf.APIError
		if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusBadRequest || apiErr.StatusCode == http.StatusNotFound) {
			return "", jf.ErrResultWithHint("Use jellyfin_search or jellyfin_browse to find valid item IDs first.", "Invalid item_ids: %v", err)
		}
	} else if out.Items != nil {
		found := make(map[string]bool, len(out.Items))
		for _, it := range out.Items {
			id := jf.NormalizeID(jf.GetString(it, "Id"))
			found[id] = true
			name := jf.GetString(it, "Name")
			if name == "" {
				continue
			}
			if res := videoStreamLabel(jf.ToSlice(it["MediaStreams"])); res != "" {
				name = fmt.Sprintf("%s [%s]", name, res)
			}
			labels[id] = name
		}
		for _, rawID := range ids {
			if !found[jf.NormalizeID(rawID)] {
				return "", jf.ErrResultWithHint("Use jellyfin_search or jellyfin_browse to find valid item IDs first.", "Item %q was not found.", rawID)
			}
		}
	}
	return fmt.Sprintf("Merge %s into alternate versions of one item? Jellyfin combines them under a single library entry (preferring a non-3D, highest-resolution version as primary).", nameList(ids, labels)), nil
}

func videoStreamLabel(streams []any) string {
	for _, raw := range streams {
		m := jf.ToMap(raw)
		if jf.GetString(m, "Type") != "Video" {
			continue
		}
		if title := jf.GetString(m, "DisplayTitle"); title != "" {
			return title
		}
		w, h := jf.GetInt(m, "Width"), jf.GetInt(m, "Height")
		if w > 0 && h > 0 {
			return fmt.Sprintf("%dx%d", w, h)
		}
	}
	return ""
}

func hasGroupedAlternateSource(item map[string]any) bool {
	for _, raw := range jf.ToSlice(item["MediaSources"]) {
		if m := jf.ToMap(raw); strings.EqualFold(jf.GetString(m, "Type"), "Grouping") {
			return true
		}
	}
	return len(jf.ToSlice(item["LinkedAlternateVersions"])) > 0
}

func inspectSplitVersions(ctx context.Context, client jf.Client, itemID string) (string, *mcp.CallToolResult) {
	name := itemID
	var item map[string]any
	userID, _ := client.GetUserID(ctx)
	params := url.Values{"Fields": {"MediaSources"}}
	if userID != "" {
		params.Set("UserId", userID)
	}
	if err := client.Get(ctx, "/Items/"+jf.SanitizeID(itemID), params, &item); err != nil {
		var apiErr *jf.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return "", jf.ErrResultWithHint("Use jellyfin_search to find the merged item ID.", "Item %q was not found.", itemID)
		}
		return fmt.Sprintf("Split the alternate versions of '%s' back into separate items?", name), nil
	}
	if n := jf.GetString(item, "Name"); n != "" {
		name = n
	}
	// Jellyfin's BaseItemDto marks manually merged alternate versions with
	// MediaSources[i].Type == "Grouping", whereas folder-grouped files in the
	// same directory all report Type == "Default".
	if item != nil && !hasGroupedAlternateSource(item) {
		if len(jf.ToSlice(item["MediaSources"])) > 1 || jf.GetInt(item, "MediaSourceCount") > 1 {
			return "", jf.ErrResult("'%s' has folder-grouped versions in the same directory rather than manually merged versions, so split_versions cannot separate them. Move the files into separate folders and scan the library instead.", name)
		}
		if item["MediaSources"] != nil || item["MediaSourceCount"] != nil {
			return "", jf.ErrResult("'%s' has no merged alternate versions to split.", name)
		}
	}
	return fmt.Sprintf("Split the alternate versions of '%s' back into separate items?", name), nil
}

func inspectSubtitleDeletion(ctx context.Context, client jf.Client, itemID string, subIndex int) (string, *mcp.CallToolResult) {
	name := itemID
	var item map[string]any
	userID, _ := client.GetUserID(ctx)
	params := url.Values{"Fields": {"MediaStreams"}}
	if userID != "" {
		params.Set("UserId", userID)
	}
	if err := client.Get(ctx, "/Items/"+jf.SanitizeID(itemID), params, &item); err != nil || item == nil {
		var apiErr *jf.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return "", jf.ErrResultWithHint("Use jellyfin_search or jellyfin_browse to find item IDs.", "Item %q was not found.", itemID)
		}
		return fmt.Sprintf("Delete external subtitle track %d from '%s'? Its subtitle file on disk is permanently removed.", subIndex, name), nil
	}
	if n := jf.GetString(item, "Name"); n != "" {
		name = n
	}
	streams := jf.ToSlice(item["MediaStreams"])
	if len(streams) == 0 {
		return fmt.Sprintf("Delete external subtitle track %d from '%s'? Its subtitle file on disk is permanently removed.", subIndex, name), nil
	}
	for _, raw := range streams {
		m := jf.ToMap(raw)
		if jf.GetInt(m, "Index") != subIndex || jf.GetString(m, "Type") != "Subtitle" {
			continue
		}
		if !jf.GetBool(m, "IsExternal") {
			return "", jf.ErrResult("Subtitle track %d on '%s' is embedded inside the media file and cannot be deleted. Only external subtitle files (is_external=true in jellyfin_get_item) can be deleted.", subIndex, name)
		}
		var meta []string
		if lang := jf.GetString(m, "Language"); lang != "" {
			meta = append(meta, lang)
		}
		if codec := jf.GetString(m, "Codec"); codec != "" {
			meta = append(meta, codec)
		}
		if len(meta) > 0 {
			return fmt.Sprintf("Delete external subtitle track %d (%s) from '%s'? Its subtitle file on disk is permanently removed.", subIndex, strings.Join(meta, ", "), name), nil
		}
		return fmt.Sprintf("Delete external subtitle track %d from '%s'? Its subtitle file on disk is permanently removed.", subIndex, name), nil
	}
	return "", jf.ErrResult("Item '%s' has no subtitle track at index %d. Check subtitle_streams in jellyfin_get_item.", name, subIndex)
}
