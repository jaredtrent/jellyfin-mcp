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

// metadataFields names the fields a metadata update or batch_update would
// write, for its preview and confirmation.
func metadataFields(args jf.MetadataInput) []string {
	var fields []string
	if args.Name != "" {
		fields = append(fields, "name")
	}
	if args.Overview != "" {
		fields = append(fields, "overview")
	}
	if len(args.Genres) > 0 {
		fields = append(fields, "genres")
	}
	if len(args.Tags) > 0 {
		fields = append(fields, "tags")
	}
	if len(args.Studios) > 0 {
		fields = append(fields, "studios")
	}
	if args.Year != nil {
		fields = append(fields, "production_year")
	}
	if args.CommunityRating != nil {
		fields = append(fields, "community_rating")
	}
	if args.OfficialRating != "" {
		fields = append(fields, "official_rating")
	}
	if args.SortName != "" {
		fields = append(fields, "sort_name")
	}
	if len(args.LockedFields) > 0 {
		fields = append(fields, "locked_fields")
	}
	return fields
}

func RegisterContentTools(server *mcp.Server, client jf.Client, enabled func(string, *mcp.ToolAnnotations) bool) {

	// --- jellyfin_videos ---
	if enabled("jellyfin_videos", AnnotDestructive) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_videos",
			Title: "Video Versions",
			InputSchema: jf.WithEnums[jf.VideosInput](map[string][]any{
				"action": {"merge_versions", "split_versions"},
			}),
			Description: "Manage video file versions. Use 'merge_versions' to combine multiple items as alternate versions of the same title " +
				"(such as 4K and 1080p copies). Use 'split_versions' to undo a merge and restore items as separate entries. " +
				"Both actions ask the user to confirm. Get item IDs from jellyfin_search or jellyfin_browse.",
			Annotations: AnnotDestructive,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.VideosInput) (*mcp.CallToolResult, any, error) {
			switch args.Action {
			case "merge_versions":
				ids := jf.SplitIDs(args.ItemIDs)
				if len(ids) < 2 {
					return jf.ErrResult("item_ids must contain at least 2 item IDs to merge."), nil, nil
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Merge %d items into alternate versions of one item? Jellyfin keeps the highest-resolution copy as the primary version.", len(ids))); result != nil {
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
				if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Split the alternate versions of '%s' back into separate items?", itemName(ctx, client, args.ItemID, ""))); result != nil {
					return result, nil, nil
				}
				endpoint := fmt.Sprintf("/Videos/%s/AlternateSources", jf.SanitizeID(args.ItemID))
				if err := client.Del(ctx, endpoint, nil); err != nil {
					return jf.ErrResult("Failed to split versions: %v", err), nil, nil
				}
				return jf.TextResult("Alternate versions split into separate items."), nil, nil

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: merge_versions, split_versions", args.Action), nil, nil
			}
		})
	}

	// --- jellyfin_metadata ---
	if enabled("jellyfin_metadata", AnnotWriteOp) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_metadata",
			Title: "Metadata",
			InputSchema: jf.WithEnums[jf.MetadataInput](map[string][]any{
				"action": {"search", "apply", "update", "batch_update", "editor_info", "external_ids"},
			}),
			Description: "Search for and manage item metadata from online providers. Use 'search' to find metadata matches, then 'apply' to identify an item as one of them. " +
				"For 'apply', provider_name is a key from the chosen result's provider_ids, such as Tmdb, Imdb, or Tvdb, rather than a provider's display name such as TheMovieDb, and provider_id is that key's value. " +
				"Applying a match replaces the item's provider IDs, metadata, and images with the match's. " +
				"Also supports 'update' for manual field edits, 'batch_update' for bulk changes, 'editor_info', and 'external_ids'.",
			Annotations: AnnotWriteOp,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.MetadataInput) (*mcp.CallToolResult, any, error) {
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
				if result := jf.ConfirmationGate(ctx, req, args.Confirm, fmt.Sprintf("Identify '%s' as %s %s? Its provider IDs, metadata, and images are replaced with the match's.", itemName(ctx, client, args.ItemID, ""), providerKey, providerID)); result != nil {
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
				fields := metadataFields(args)
				if len(fields) == 0 {
					return jf.ErrResult("No fields to update. Provide at least one field (name, overview, genres, tags, studios, production_year, community_rating, official_rating, sort_name, locked_fields)."), nil, nil
				}
				userID, err := client.GetUserID(ctx)
				if err != nil {
					return jf.ErrResult("Jellyfin error: %v", err), nil, nil
				}
				name := itemName(ctx, client, args.ItemID, userID)
				if args.DryRun != nil && *args.DryRun {
					return jf.TextResult(fmt.Sprintf("Dry run, no changes made. An update of '%s' writes these fields: %s\n\nCall again without dry_run to apply; the user is asked to confirm.", name, strings.Join(fields, ", "))), nil, nil
				}
				if result := jf.ConfirmationGate(ctx, req, args.Confirm, fmt.Sprintf("Update the metadata of '%s'? Fields written: %s.", name, strings.Join(fields, ", "))); result != nil {
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
				fields := metadataFields(args)
				if len(fields) == 0 {
					return jf.ErrResult("No fields to update. Provide at least one field (name, overview, genres, tags, studios, production_year, community_rating, official_rating, sort_name, locked_fields)."), nil, nil
				}

				// Dry-run mode: default true — preview changes without applying
				dryRun := args.DryRun == nil || *args.DryRun
				if dryRun {
					return jf.TextResult(fmt.Sprintf("Dry run, no changes made. An update of %d %s writes these fields: %s\n\nSet dry_run=false to apply; the user is asked to confirm.", len(args.ItemIDs), plural(len(args.ItemIDs), "item", "items"), strings.Join(fields, ", "))), nil, nil
				}

				if result := jf.ConfirmationGate(ctx, req, args.Confirm, fmt.Sprintf("Update the metadata of %d %s? Fields written: %s.", len(args.ItemIDs), plural(len(args.ItemIDs), "item", "items"), strings.Join(fields, ", "))); result != nil {
					return result, nil, nil
				}

				userID, err := client.GetUserID(ctx)
				if err != nil {
					return jf.ErrResult("Jellyfin error: %v", err), nil, nil
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

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: search, apply, update, batch_update, editor_info, external_ids", args.Action), nil, nil
			}
		})
	}

	// --- jellyfin_subtitles_lyrics ---
	if enabled("jellyfin_subtitles_lyrics", AnnotWriteOp) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_subtitles_lyrics",
			Title: "Subtitles & Lyrics",
			InputSchema: jf.WithEnums[jf.SubtitlesLyricsInput](map[string][]any{
				"action": {"search_subtitles", "download_subtitle", "delete_subtitle", "batch_download_subtitles", "upload_subtitle", "get_lyrics", "search_lyrics", "download_lyrics", "delete_lyrics"},
			}),
			Description: "Search for, download, and manage subtitles and lyrics. For subtitles: use 'search_subtitles' with item_id and language to find available subtitles online, " +
				"'download_subtitle' with subtitle_id to download one, 'delete_subtitle' with subtitle_index to remove a track (asks the user to confirm). " +
				"Use 'batch_download_subtitles' with item_ids (max 25) to search and download subtitles for multiple items (asks the user to confirm). " +
				fmt.Sprintf("Use 'upload_subtitle' with item_id, subtitle_data (the base64-encoded file, at most %d MiB), and subtitle_format, which must be one of ass, mks, sami, smi, srt, ssa, sub, sup, or vtt. ", maxUploadBytes>>20) +
				"For lyrics: use 'get_lyrics' to view current lyrics, 'search_lyrics' to find lyrics online, " +
				"'download_lyrics' with lyric_id to download, 'delete_lyrics' to remove (asks the user to confirm). Language codes use ISO 639 format (such as en, es, fr, de, ja).",
			Annotations: AnnotWriteOp,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.SubtitlesLyricsInput) (*mcp.CallToolResult, any, error) {
			itemID := jf.SanitizeID(args.ItemID)

			switch args.Action {
			case "search_subtitles":
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
				endpoint := fmt.Sprintf("/Items/%s/RemoteSearch/Subtitles/%s", itemID, jf.SanitizeID(args.SubtitleID))
				if err := client.PostNoContent(ctx, endpoint, nil, nil); err != nil {
					return jf.ErrResult("Failed to download subtitle: %v", err), nil, nil
				}
				return jf.TextResult("Subtitle downloaded and added to item."), nil, nil

			case "delete_subtitle":
				if args.SubtitleIndex == nil {
					return jf.ErrResult("subtitle_index is required. Get the index from jellyfin_get_item media source info."), nil, nil
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Delete subtitle track %d from '%s'? Its subtitle file is removed.", *args.SubtitleIndex, itemName(ctx, client, args.ItemID, ""))); result != nil {
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
				if result := jf.ConfirmationGate(ctx, req, args.Confirm, fmt.Sprintf("Search for and download subtitles for %d %s in language '%s'? The first match for each item is downloaded.", len(args.ItemIDs), plural(len(args.ItemIDs), "item", "items"), lang)); result != nil {
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
				endpoint := fmt.Sprintf("/Audio/%s/Lyrics", itemID)
				var result map[string]any
				if err := client.Get(ctx, endpoint, nil, &result); err != nil {
					return jf.ErrResult("Jellyfin API error: %v. This item may not have lyrics.", err), nil, nil
				}
				return jf.TextResult(jf.FormatJSON(result)), nil, nil

			case "search_lyrics":
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
				endpoint := fmt.Sprintf("/Audio/%s/RemoteSearch/Lyrics/%s", itemID, jf.SanitizeID(args.LyricID))
				if err := client.PostNoContent(ctx, endpoint, nil, nil); err != nil {
					return jf.ErrResult("Failed to download lyrics: %v", err), nil, nil
				}
				return jf.TextResult("Lyrics downloaded and added to item."), nil, nil

			case "upload_subtitle":
				if args.ItemID == "" {
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
				if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Delete the lyrics of '%s'? The lyrics file is removed.", itemName(ctx, client, args.ItemID, ""))); result != nil {
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
	if enabled("jellyfin_images", AnnotWriteOp) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_images",
			Title: "Images",
			InputSchema: jf.WithEnums[jf.ImagesInput](map[string][]any{
				"action": {"list", "get_url", "remote_list", "remote_download", "upload"},
			}),
			Description: "Manage item images. Use 'list' to see all images for an item (types: Primary, Backdrop, Logo, Thumb, Banner). " +
				"Use 'get_url' to get the direct URL for an item's image (useful for displaying or linking). " +
				"Use 'remote_list' to browse available images from online providers like TheMovieDb. " +
				"Use 'remote_download' to download a specific remote image URL and set it for the item. " +
				fmt.Sprintf("Use 'upload' to set an image from base64-encoded data: a JPEG, PNG, WebP, GIF, or BMP file of at most %d MiB.", maxUploadBytes>>20),
			Annotations: AnnotWriteOp,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.ImagesInput) (*mcp.CallToolResult, any, error) {
			itemID := jf.SanitizeID(args.ItemID)
			imageType := args.ImageType
			if imageType == "" {
				imageType = "Primary"
			}
			if !slices.Contains(imageTypes, imageType) {
				return jf.ErrResult("image_type %q is not a Jellyfin image type. Valid types: %s.", args.ImageType, strings.Join(imageTypes, ", ")), nil, nil
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
