package tools

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	jf "github.com/jaredtrent/jellyfin-mcp/internal/jellyfin"
)

func RegisterAdminLibraryTools(server *mcp.Server, client jf.Client, enabled func(string, *mcp.ToolAnnotations) bool) {

	// --- jellyfin_library_manage ---
	if enabled("jellyfin_library_manage", AnnotDestructive) {
		mcp.AddTool(server, &mcp.Tool{
			Name:  "jellyfin_library_manage",
			Title: "Library Management",
			InputSchema: jf.WithEnums[jf.LibraryManageInput](map[string][]any{
				"action": {"scan", "scan_path", "refresh_item", "delete_item", "list_folders", "add_folder", "remove_folder", "add_path", "remove_path", "rename_folder", "update_options", "browse_drives", "browse_directory"},
			}),
			Description: "Manage media libraries: scan for new content, refresh metadata, delete items, and manage library folders/paths. " +
				"Use 'scan' to trigger a full library scan for new or changed content, or 'scan_path' to report one path as changed and have Jellyfin read the nearest item that holds it: an item's own path reads that item, and a folder that is no item itself reads the library around it. " +
				"Use 'refresh_item' to re-fetch metadata and images for a specific item (with a library's item_id, it rescans that library). " +
				"Use 'delete_item' to permanently remove an item (destructive). Use 'list_folders' to see library configuration. " +
				"Use 'add_folder'/'remove_folder' to create or delete libraries, and 'add_path'/'remove_path' to manage media paths within a library. " +
				"When 'add_folder' is given a path, the new library starts with that media path and a library scan is requested. " +
				"Use 'rename_folder' to rename a library. Use 'update_options' to set library options (use list_folders to see current options). " +
				"Use 'browse_drives' to list server drives/mount points, or 'browse_directory' to browse the server filesystem (admin-only).",
			Annotations: AnnotDestructive,
		}, func(ctx context.Context, req *mcp.CallToolRequest, args jf.LibraryManageInput) (*mcp.CallToolResult, any, error) {
			switch args.Action {
			case "scan":
				if err := client.PostNoContent(ctx, "/Library/Refresh", nil, nil); err != nil {
					return jf.ErrResult("Failed to start library scan: %v", err), nil, nil
				}
				return jf.TextResult("Library scan started. This runs in the background and may take several minutes for large libraries."), nil, nil

			case "scan_path":
				if args.Path == "" {
					return jf.ErrResult("path is required for scan_path."), nil, nil
				}
				// Jellyfin hands the path to its library monitor and reads
				// nothing else from this body: UpdateType is documented as
				// Created, Modified, or Deleted, but no version consults it,
				// so it is left out rather than sent as a guess.
				body := map[string]any{"Updates": []map[string]any{{"Path": args.Path}}}
				if err := client.PostNoContent(ctx, "/Library/Media/Updated", nil, body); err != nil {
					return jf.ErrResult("Failed to report the changed path: %v", err), nil, nil
				}
				// The monitor waits out its debounce before it reads anything,
				// because a folder being written is not complete yet. It reads
				// the nearest item holding the path, which is the library when
				// no item stands on the path itself: a movie is its file, so a
				// movie's folder reads the library that holds it.
				return jf.TextResult(fmt.Sprintf("Jellyfin was told '%s' changed. After the library monitor's delay, 60 seconds by default, it reads the nearest item that holds the path: that item, or the library around it when no item stands on the path itself. A path no library holds is ignored.", args.Path)), nil, nil

			case "refresh_item":
				if args.ItemID == "" {
					return jf.ErrResult("item_id is required for refresh_item."), nil, nil
				}
				// Without a refresh mode Jellyfin runs no metadata provider, so
				// both modes are always FullRefresh; the flags decide whether
				// existing metadata and images are replaced or only filled in.
				params := url.Values{
					"MetadataRefreshMode": {"FullRefresh"},
					"ImageRefreshMode":    {"FullRefresh"},
				}
				if args.ReplaceMetadata != nil && *args.ReplaceMetadata {
					params.Set("ReplaceAllMetadata", "true")
				}
				if args.ReplaceImages != nil && *args.ReplaceImages {
					params.Set("ReplaceAllImages", "true")
				}
				endpoint := fmt.Sprintf("/Items/%s/Refresh", jf.SanitizeID(args.ItemID))
				if err := client.PostNoContent(ctx, endpoint, params, nil); err != nil {
					return jf.ErrResult("Failed to refresh item: %v", err), nil, nil
				}
				return jf.TextResult("Item metadata refresh started."), nil, nil

			case "delete_item":
				if args.ItemID == "" {
					return jf.ErrResult("item_id is required for delete_item."), nil, nil
				}
				name := itemName(ctx, client, args.ItemID, "")
				if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Delete '%s' and its files permanently? There is no undo.", name)); result != nil {
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
					return jf.ErrResult("library_options is required for update_options: the complete LibraryOptions object from list_folders, edited."), nil, nil
				}
				if result := jf.DestructiveGate(ctx, req, args.Confirm, fmt.Sprintf("Replace all options of library '%s' with the library_options given? Options it leaves out return to their defaults.", args.FolderName)); result != nil {
					return result, nil, nil
				}
				// Find library ID from name
				var libs []map[string]any
				if err := client.Get(ctx, "/Library/VirtualFolders", nil, &libs); err != nil {
					return jf.ErrResult("Jellyfin API error: %v", err), nil, nil
				}
				var libID string
				for _, lib := range libs {
					if jf.GetString(lib, "Name") == args.FolderName {
						libID = jf.GetString(lib, "ItemId")
						break
					}
				}
				if libID == "" {
					return jf.ErrResult("Library '%s' not found. Use list_folders to see available libraries.", args.FolderName), nil, nil
				}
				body := map[string]any{
					"Id":             libID,
					"LibraryOptions": args.LibraryOptions,
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
				return jf.ErrResult("Invalid action '%s'. Valid actions: scan, scan_path, refresh_item, delete_item, list_folders, add_folder, remove_folder, add_path, remove_path, rename_folder, update_options, browse_drives, browse_directory", args.Action), nil, nil
			}
		})
	}
}
