package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

func RegisterAdminLibraryTools(server *mcp.Server, client jf.Client, enabled func(string, *mcp.ToolAnnotations) bool) {

	// --- jellyfin_library_manage ---
	full := enabled("jellyfin_library_manage", AnnotDestructive)
	curate := !full && enabled("jellyfin_library_manage", annotCurateProbe)
	ro := enabled("jellyfin_library_manage", AnnotReadOnly)
	if full || curate || ro {
		var allowedActions []any
		var modeLabel string
		schema := jf.WithEnums[jf.LibraryManageInput](map[string][]any{
			"action": {"scan", "refresh_item", "delete_item", "list_folders", "add_folder", "remove_folder", "add_path", "remove_path", "rename_folder", "update_options", "browse_drives", "browse_directory"},
		})
		desc := "Scan libraries or changed server paths, refresh item metadata/images, delete items/files, configure virtual library folders and paths, or browse server drives/directories (admin). " +
			"Use scan (empty for all libraries, item_id for immediate scan, or path for library-monitor change notice) for new/changed files; refresh_item re-fetches provider metadata/images."
		annot := AnnotDestructive

		switch {
		case curate:
			allowedActions = []any{"list_folders", "scan", "refresh_item"}
			desc = "Inspect virtual library folders, scan libraries or changed server paths for new/updated media files, or refresh item metadata/images."
			if ro {
				allowedActions = append(allowedActions, "browse_drives", "browse_directory")
				desc = "Inspect virtual library folders, browse server drives and directories, scan libraries or changed server paths, or refresh item metadata/images."
			}
			schema = jf.WithEnumsFiltered[jf.LibraryManageInput](
				map[string][]any{"action": allowedActions},
				[]string{"action", "item_id", "path", "replace_all_metadata", "replace_all_images", "confirm"},
			)
			modeLabel = "curator mode (allowed actions"
		case !full && ro:
			allowedActions = []any{"list_folders", "browse_drives", "browse_directory"}
			schema = jf.WithEnumsFiltered[jf.LibraryManageInput](map[string][]any{"action": allowedActions}, []string{"action", "path"})
			desc = "Inspect library virtual-folder configuration or browse server drives and directories (admin)."
			annot = AnnotReadOnly
			modeLabel = "read-only mode (allowed read-only actions"
		}
		mcp.AddTool(server, &mcp.Tool{
			Name:        "jellyfin_library_manage",
			Title:       "Library Management",
			InputSchema: schema,
			Description: desc,
			Annotations: annot,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.LibraryManageInput) (*mcp.CallToolResult, any, error) {
			if len(allowedActions) > 0 {
				if res := requireAllowedAction(args.Action, allowedActions, modeLabel); res != nil {
					return res, nil, nil
				}
			}
			switch args.Action {
			case "scan":
				switch {
				// The replace flags belong to refresh_item. Jellyfin documents
				// them for a full refresh only, and replace_all_metadata would
				// make every provider run on every item even in a scan, so a
				// scan refuses them rather than dropping them unnoticed.
				case (args.ReplaceMetadata != nil && *args.ReplaceMetadata) || (args.ReplaceImages != nil && *args.ReplaceImages):
					return jf.ErrResult("A scan reads files and replaces no metadata or images. To replace them, use refresh_item with replace_all_metadata or replace_all_images."), nil, nil

				case args.ItemID != "" && args.Path != "":
					return jf.ErrResult("Give scan either item_id or path, not both: with item_id it scans that item right away, and with path it reports a change that Jellyfin reads after its library monitor delay."), nil, nil

				case args.ItemID != "":
					// Default for both modes is what the dashboard's Scan Library
					// sends: Jellyfin reads the files under the item again, and its
					// providers run only for items that are new, changed, or due for
					// a refresh.
					targetID, err := jf.ResolveLibraryID(ctx, client, args.ItemID)
					if err != nil {
						return jf.ResolveErrResult(err), nil, nil
					}
					params := url.Values{
						"MetadataRefreshMode": {"Default"},
						"ImageRefreshMode":    {"Default"},
					}
					if err := client.PostNoContent(ctx, fmt.Sprintf("/Items/%s/Refresh", jf.SanitizeID(targetID)), params, nil); err != nil {
						return jf.ErrResult("Failed to start the scan: %v", err), nil, nil
					}
					return jf.TextResult("Scan started. Jellyfin reads the files under this item again: it adds new items, updates changed ones, and removes those whose files are gone. Metadata providers run only for new or changed items and for items due for a refresh. The scan runs in the background. When the item is a library, list_folders shows the scan's state as the library's RefreshStatus (Queued, Active, or Idle) and its percent complete as RefreshProgress."), nil, nil

				case args.Path != "":
					// Jellyfin accepts any path here and resolves it later, when
					// its library monitor's delay runs out, to the nearest item at
					// or above the path. A path outside every library folder
					// resolves to nothing, so it's refused while the caller can
					// still correct it.
					var libs []struct {
						Name      string
						Locations []string
					}
					if err := client.Get(ctx, "/Library/VirtualFolders", nil, &libs); err != nil {
						return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
					}
					// The longest matching folder names the library, because one
					// library's folder can sit inside another's.
					var library, holder, report string
					var folders []string
					for _, lib := range libs {
						for _, location := range lib.Locations {
							if sent, ok := pathUnder(args.Path, location); ok && (library == "" || len(location) > len(holder)) {
								library, holder, report = lib.Name, location, sent
							}
							folders = append(folders, fmt.Sprintf("'%s' (%s)", location, lib.Name))
						}
					}
					if library == "" {
						if len(folders) == 0 {
							return jf.ErrResult("'%s' isn't inside a library folder, because no library on the server has a folder. Use add_path to give a library a folder, or add_folder to create a library with one.", args.Path), nil, nil
						}
						return jf.ErrResult("'%s' isn't inside a library folder, so Jellyfin would ignore it. Give the path as the Jellyfin server sees it, with the same letter case, inside one of these folders: %s. To scan every library, leave path unset.", args.Path, strings.Join(folders, ", ")), nil, nil
					}
					// Jellyfin reads only Path from each update. UpdateType is
					// documented as Created, Modified, or Deleted, but the server
					// never consults it.
					body := map[string]any{"Updates": []map[string]any{{"Path": report}}}
					if err := client.PostNoContent(ctx, "/Library/Media/Updated", nil, body); err != nil {
						return jf.ErrResult("Failed to report the changed path: %v", err), nil, nil
					}
					// The library monitor waits out its delay before it reads, so
					// that a folder still being written settles first. The delay
					// is a server setting, read back so the result states it.
					when := "After the server's library monitor delay, 60 seconds by default"
					var config struct{ LibraryMonitorDelay *int }
					if err := client.Get(ctx, "/System/Configuration", nil, &config); err == nil && config.LibraryMonitorDelay != nil {
						unit := "seconds"
						if *config.LibraryMonitorDelay == 1 {
							unit = "second"
						}
						when = fmt.Sprintf("After the server's library monitor delay of %d %s", *config.LibraryMonitorDelay, unit)
					}
					return jf.TextResult(fmt.Sprintf("Jellyfin queues a read of '%s' in the %s library. %s, it reads the nearest item at or above the path. That's the item at the path when one exists; otherwise it's the closest item above it, such as a show's season, or the library folder when nothing in between is an item, as with a movie's folder. The read runs in the background, and new items appear in the library when it finishes.", report, library, when)), nil, nil
				}
				if err := client.PostNoContent(ctx, "/Library/Refresh", nil, nil); err != nil {
					return jf.ErrResult("Failed to start library scan: %v", err), nil, nil
				}
				return jf.TextResult("Library scan started. This runs in the background and may take several minutes for large libraries."), nil, nil

			case "refresh_item":
				if args.ItemID == "" {
					return jf.ErrResult("item_id is required for refresh_item."), nil, nil
				}
				replaceMeta := args.ReplaceMetadata != nil && *args.ReplaceMetadata
				replaceImg := args.ReplaceImages != nil && *args.ReplaceImages
				if replaceMeta || replaceImg {
					if res := jf.DestructiveDisabledError(ctx); res != nil {
						return res, nil, nil
					}
				}
				targetID, err := jf.ResolveLibraryID(ctx, client, args.ItemID)
				if err != nil {
					return jf.ResolveErrResult(err), nil, nil
				}
				if replaceMeta || replaceImg {
					name, itemType, _, isFolder, notFound := itemBrief(ctx, client, targetID)
					if notFound {
						return jf.ErrResultWithHint("Use jellyfin_search or jellyfin_browse to find item IDs.", "Item %q was not found.", args.ItemID), nil, nil
					}
					target := refreshScopeLabel(name, itemType, isFolder)
					var overwrite string
					switch {
					case replaceMeta && replaceImg:
						overwrite = "all existing metadata (including custom edits) and images"
					case replaceMeta:
						overwrite = "all existing metadata, including custom edits"
					default:
						overwrite = "all existing images"
					}
					warning := fmt.Sprintf("Refresh %s and overwrite %s?", target, overwrite)
					if result := jf.DestructiveGate(ctx, req, args.Confirm, warning); result != nil {
						return result, nil, nil
					}
				}
				// Without a refresh mode Jellyfin runs no metadata provider, so
				// both modes are always FullRefresh; the flags decide whether
				// existing metadata and images are replaced or only filled in.
				// On a library or folder, Jellyfin applies the modes to every
				// item under it.
				params := url.Values{
					"MetadataRefreshMode": {"FullRefresh"},
					"ImageRefreshMode":    {"FullRefresh"},
				}
				if replaceMeta {
					params.Set("ReplaceAllMetadata", "true")
				}
				if replaceImg {
					params.Set("ReplaceAllImages", "true")
				}
				if err := client.PostNoContent(ctx, fmt.Sprintf("/Items/%s/Refresh", jf.SanitizeID(targetID)), params, nil); err != nil {
					return jf.ErrResult("Failed to refresh item: %v", err), nil, nil
				}
				return jf.TextResult("Item metadata refresh started."), nil, nil

			case "delete_item":
				if args.ItemID == "" {
					return jf.ErrResult("item_id is required for delete_item."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				name, itemType, itemPath, isFolder, notFound := itemBrief(ctx, client, args.ItemID)
				if notFound {
					return jf.ErrResultWithHint("Use jellyfin_search or jellyfin_browse to find item IDs.", "Item %q was not found.", args.ItemID), nil, nil
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, deleteItemWarning(name, itemType, itemPath, isFolder)); result != nil {
					return result, nil, nil
				}
				endpoint := fmt.Sprintf("/Items/%s", jf.SanitizeID(args.ItemID))
				if err := client.Del(ctx, endpoint, nil); err != nil {
					return jf.ErrResult("Failed to delete item: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("'%s' deleted permanently.", name)), nil, nil

			case "list_folders":
				var folders []map[string]any
				if err := client.Get(ctx, "/Library/VirtualFolders", nil, &folders); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Virtual folders (%d):\n\n%s", len(folders), jf.FormatJSON(folders))), nil, nil

			case "add_folder":
				// Jellyfin 12 rejects a library name with leading or trailing
				// whitespace, and Jellyfin 10.11 trims it, so the name is trimmed
				// here to behave the same on both.
				name := strings.TrimSpace(args.FolderName)
				if name == "" {
					return jf.ErrResult("folder_name is required for add_folder."), nil, nil
				}
				params := url.Values{"name": {name}}
				if args.CollectionType != "" {
					params.Set("collectionType", args.CollectionType)
				}
				// The initial path goes in LibraryOptions.PathInfos, as jellyfin-web
				// sends it. The server splits the endpoint's "paths" query parameter
				// on commas and trims each part, which would corrupt a path that
				// contains a comma or edge whitespace. The server scans a new library
				// right away only when refreshLibrary is set, so a library created
				// with a path requests that scan.
				body := map[string]any{}
				if args.Path != "" {
					body["LibraryOptions"] = map[string]any{
						"PathInfos": []map[string]any{{"Path": args.Path}},
					}
					params.Set("refreshLibrary", "true")
				}
				if err := client.PostNoContent(ctx, "/Library/VirtualFolders", params, body); err != nil {
					return jf.ErrResult("Failed to add folder: %v", err), nil, nil
				}
				if args.Path == "" {
					return jf.TextResult(fmt.Sprintf("Library '%s' created with no media paths. Use add_path to add one.", name)), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Library '%s' created with path '%s'. A library scan was requested to import its media.", name, args.Path)), nil, nil

			case "remove_folder":
				if args.FolderName == "" {
					return jf.ErrResult("folder_name is required for remove_folder."), nil, nil
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Remove the library '%s' and its configuration? Media files on disk stay where they are.", args.FolderName)); result != nil {
					return result, nil, nil
				}
				params := url.Values{"name": {args.FolderName}}
				if err := client.Del(ctx, "/Library/VirtualFolders", params); err != nil {
					return jf.ErrResult("Failed to remove folder: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Library '%s' removed.", args.FolderName)), nil, nil

			case "add_path":
				if args.FolderName == "" || args.Path == "" {
					return jf.ErrResult("folder_name and path are required for add_path."), nil, nil
				}
				body := map[string]any{
					"Name":     args.FolderName,
					"PathInfo": map[string]any{"Path": args.Path},
				}
				if err := client.PostNoContent(ctx, "/Library/VirtualFolders/Paths", nil, body); err != nil {
					return jf.ErrResult("Failed to add path: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Path '%s' added to library '%s'.", args.Path, args.FolderName)), nil, nil

			case "remove_path":
				if args.FolderName == "" || args.Path == "" {
					return jf.ErrResult("folder_name and path are required for remove_path."), nil, nil
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Remove path '%s' from library '%s'? Media at this path disappears from the library, and the files stay on disk.", args.Path, args.FolderName)); result != nil {
					return result, nil, nil
				}
				params := url.Values{
					"name": {args.FolderName},
					"path": {args.Path},
				}
				if err := client.Del(ctx, "/Library/VirtualFolders/Paths", params); err != nil {
					return jf.ErrResult("Failed to remove path: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Path '%s' removed from library '%s'.", args.Path, args.FolderName)), nil, nil

			case "rename_folder":
				if args.FolderName == "" || args.NewName == "" {
					return jf.ErrResult("folder_name (current name) and new_name are required for rename_folder."), nil, nil
				}
				params := url.Values{
					"name":    {args.FolderName},
					"newName": {args.NewName},
				}
				if err := client.PostNoContent(ctx, "/Library/VirtualFolders/Name", params, nil); err != nil {
					return jf.ErrResult("Failed to rename library: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Library renamed from '%s' to '%s'.", args.FolderName, args.NewName)), nil, nil

			case "update_options":
				if args.FolderName == "" {
					return jf.ErrResult("folder_name is required for update_options. Use list_folders to find library names."), nil, nil
				}
				if args.LibraryOptions == nil {
					return jf.ErrResult("library_options is required for update_options: provide a JSON object of LibraryOptions fields to update (use list_folders to inspect current options)."), nil, nil
				}
				if res := jf.DestructiveDisabledError(ctx); res != nil {
					return res, nil, nil
				}
				patch, ok := jf.AsConfigMap(args.LibraryOptions)
				if !ok || len(patch) == 0 {
					return jf.ErrResult("library_options must be a non-empty JSON object of LibraryOptions fields to update (use list_folders to inspect current options)."), nil, nil
				}
				var libs []map[string]any
				if err := client.Get(ctx, "/Library/VirtualFolders", nil, &libs); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				var libID string
				var current map[string]any
				for _, lib := range libs {
					if jf.GetString(lib, "Name") == args.FolderName {
						libID = jf.GetString(lib, "ItemId")
						current, _ = jf.AsConfigMap(lib["LibraryOptions"])
						break
					}
				}
				if libID == "" {
					return jf.ErrResult("Library '%s' not found. Use list_folders to see available libraries.", args.FolderName), nil, nil
				}
				merged := jf.MergeConfig(current, patch)
				changes := jf.DiffConfig(current, merged)
				if len(current) > 0 && len(changes) == 0 {
					return jf.TextResult(fmt.Sprintf("Library '%s' already has those options; nothing changed.", args.FolderName)), nil, nil
				}
				var warning string
				if len(current) > 0 {
					warning = fmt.Sprintf("Update options for library '%s'? Changes: %s. Other library options stay unchanged.", args.FolderName, jf.FormatConfigChanges(changes))
				} else {
					warning = fmt.Sprintf("Update options for library '%s'? Settings: %s.", args.FolderName, jf.FormatConfigChanges(changes))
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, warning); result != nil {
					return result, nil, nil
				}
				body := map[string]any{
					"Id":             libID,
					"LibraryOptions": merged,
				}
				if err := client.PostNoContent(ctx, "/Library/VirtualFolders/LibraryOptions", nil, body); err != nil {
					return jf.ErrResult("Failed to update library options: %v", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Library options updated for '%s'.", args.FolderName)), nil, nil

			case "browse_drives":
				var drives any
				if err := client.Get(ctx, "/Environment/Drives", nil, &drives); err != nil {
					return jf.ErrResult("Jellyfin API error: %v. This action requires admin privileges.", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Server drives:\n\n%s", jf.FormatJSON(drives))), nil, nil

			case "browse_directory":
				if args.Path == "" {
					return jf.ErrResult("path is required for browse_directory."), nil, nil
				}
				params := url.Values{"path": {args.Path}, "includeFiles": {"true"}, "includeDirectories": {"true"}}
				var contents any
				if err := client.Get(ctx, "/Environment/DirectoryContents", params, &contents); err != nil {
					return jf.ErrResult("Jellyfin API error: %v. This action requires admin privileges.", err), nil, nil
				}
				return jf.TextResult(fmt.Sprintf("Directory contents of '%s':\n\n%s", args.Path, jf.FormatJSON(contents))), nil, nil

			default:
				return jf.ErrResult("Invalid action '%s'. Valid actions: scan, refresh_item, delete_item, list_folders, add_folder, remove_folder, add_path, remove_path, rename_folder, update_options, browse_drives, browse_directory", args.Action), nil, nil
			}
		})
	}
}

// pathUnder reports whether path is folder or lies under it, and returns the
// path to report. Paths compare the way Jellyfin resolves a reported path,
// letter for letter, because it looks each path up by exact match, on Windows
// too. Under a Windows folder either separator counts, because the server's
// own path handling reads both, and the path is reported with backslashes, the
// separator Jellyfin stores, so that its first lookup can find the item at the
// path itself.
func pathUnder(path, folder string) (string, bool) {
	if folder == "" {
		return path, false
	}
	sep := "/"
	if windowsPath(folder) {
		sep = `\`
		path = strings.ReplaceAll(path, "/", sep)
		folder = strings.ReplaceAll(folder, "/", sep)
	}
	folder = strings.TrimRight(folder, sep)
	return path, path == folder || strings.HasPrefix(path, folder+sep)
}

// windowsPath reports whether a library folder is a Windows path, one that
// starts with a drive letter or names a network share.
func windowsPath(folder string) bool {
	return strings.HasPrefix(folder, `\\`) || len(folder) >= 2 && folder[1] == ':'
}

// itemBrief looks up an item's name, Jellyfin type, filesystem path, and folder flag
// for confirmation messages, falling back to itemID when the lookup fails and
// reporting notFound when Jellyfin returns HTTP 404.
func itemBrief(ctx context.Context, client jf.Client, itemID string) (name, itemType, itemPath string, isFolder, notFound bool) {
	name = itemID
	var item map[string]any
	userID, _ := client.GetUserID(ctx)
	params := url.Values{"Fields": {"Path"}}
	if userID != "" {
		params.Set("UserId", userID)
	}
	if err := client.Get(ctx, "/Items/"+jf.SanitizeID(itemID), params, &item); err != nil {
		var apiErr *jf.APIError
		return name, "", "", false, errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
	}
	if n := jf.GetString(item, "Name"); n != "" {
		name = n
	}
	itemType = jf.GetString(item, "Type")
	itemPath = jf.GetString(item, "Path")
	isFolder = jf.GetBool(item, "IsFolder")
	return name, itemType, itemPath, isFolder, false
}

func itemTypeWord(itemType, fallback string) string {
	switch itemType {
	case "CollectionFolder", "UserView":
		return "library"
	case "BoxSet":
		return "collection"
	case "MusicAlbum":
		return "album"
	case "MusicArtist":
		return "artist"
	case "MusicVideo":
		return "music video"
	case "":
		return fallback
	default:
		return strings.ToLower(itemType)
	}
}

func refreshScopeLabel(name, itemType string, isFolder bool) string {
	switch itemType {
	case "CollectionFolder", "UserView":
		return fmt.Sprintf("library '%s' and every item in it", name)
	case "Series", "Season", "Folder", "BoxSet", "MusicAlbum", "MusicArtist":
		return fmt.Sprintf("%s '%s' and every item under it", itemTypeWord(itemType, "folder"), name)
	}
	if isFolder {
		return fmt.Sprintf("folder '%s' and every item under it", name)
	}
	if kind := itemTypeWord(itemType, ""); kind != "" {
		return fmt.Sprintf("%s '%s'", kind, name)
	}
	return fmt.Sprintf("'%s'", name)
}

func deleteItemWarning(name, itemType, itemPath string, isFolder bool) string {
	recursive := isFolder || itemType == "Series" || itemType == "Season" || itemType == "Folder"
	if recursive {
		kind := itemTypeWord(itemType, "folder")
		if itemPath != "" {
			return fmt.Sprintf("Delete %s '%s' and all media files under '%s' permanently? Every child item and its files on disk are deleted; there is no undo.", kind, name, itemPath)
		}
		return fmt.Sprintf("Delete %s '%s' and all child items and files permanently? There is no undo.", kind, name)
	}
	kind := itemTypeWord(itemType, "")
	if itemPath != "" {
		if kind != "" {
			return fmt.Sprintf("Delete %s '%s' ('%s') permanently? Its file on disk is deleted and there is no undo.", kind, name, itemPath)
		}
		return fmt.Sprintf("Delete '%s' ('%s') permanently? Its file on disk is deleted and there is no undo.", name, itemPath)
	}
	if kind != "" {
		return fmt.Sprintf("Delete %s '%s' and its files permanently? There is no undo.", kind, name)
	}
	return fmt.Sprintf("Delete '%s' and its files permanently? There is no undo.", name)
}
