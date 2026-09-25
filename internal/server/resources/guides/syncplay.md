# SyncPlay (Watch Together)

SyncPlay keeps several Jellyfin clients playing the same queue at the same moment. One person creates a group and the others join it. Play, pause, seek, and queue changes from any member apply to the whole group. Members can be different users, and one user can join from several devices.

## Supported Clients

| Client | SyncPlay support |
|--------|------------------|
| Jellyfin web client (browser) | Create, join, and control groups |
| Jellyfin Desktop, named Jellyfin Media Player in its stable releases | Supported since version 1.1.0 |
| Jellyfin for Android | Supported through the app's web interface. Against Jellyfin 12.1 the app often never reports ready, so a group it joins waits out the 30-second limit and pauses for everyone |
| Jellyfin for Android TV | Not supported |
| Swiftfin (iOS and tvOS) | Not supported for playback; its admin screens can change a user's SyncPlay access |

The table lists only the clients whose SyncPlay support has been checked; for any other client, check its own documentation. A client without SyncPlay cannot join a group, so every member needs a supported client.

## Permissions

Each user has a SyncPlay access level, stored in the user policy as `SyncPlayAccess`.

| Dashboard label | Policy value | Effect |
|-----------------|--------------|--------|
| Allow user to create and join groups | CreateAndJoinGroups | Can create and join groups. New users start with this level. |
| Allow user to join groups | JoinGroups | Can join existing groups but cannot create one |
| Disabled for this user | None | Cannot use SyncPlay; the web client hides the SyncPlay button |

To change the level, open Dashboard > Users, select the user, set SyncPlay access on the Profile tab, and save. The `jellyfin_users` tool of this server does not show or change this setting.

## Create and Join a Group in the Web Client

1. If you want the group to start with a queue, start playing it first. A group created while its creator is playing something starts with that queue and position.
2. Select the SyncPlay button, a group icon in the header.
3. To start a group, choose New group (Create a new group in some layouts). The group is named after its creator's user name.
4. On each other client, open the same menu and select the group to join it.
5. While in a group, starting playback of any item replaces the queue for every member.
6. Open the SyncPlay menu again to leave the group, open the SyncPlay settings, or stop and resume local playback. Stop local playback stops that client only and tells the server not to wait for it.

## How the Group Stays in Sync

The server pauses the group and waits for every member to report that it is ready when someone joins a group that is playing or paused, when a new queue starts, after a seek or a change of item, and whenever a member reports that it is buffering. One slow or stalled member therefore holds up everyone.

- Pressing play while the group waits makes it resume once every member is ready. If the group was already set to resume, pressing play starts playback for the whole group at once instead of waiting for the members that are not ready, so a group that was paused before the wait needs a second press.
- When the member the group is waiting for leaves, or chooses Stop local playback, the others carry on.
- Jellyfin 12.1 gives up waiting after about 30 seconds. It logs a warning that the group gave up waiting for sessions to report ready. It then starts playback for the whole group if the group was set to resume, and otherwise leaves the group paused. Jellyfin 10.11 and 12.0 have no time limit and wait until every member is ready or leaves.
- Sync Correction in the SyncPlay settings is off by default and is set separately on each client. When it is on, the client corrects drift by briefly changing playback speed (SpeedToSync) or by seeking (SkipToSync).

## Network Requirements

The server sends SyncPlay commands and group updates to each client only over that client's WebSocket connection. Browsing and ordinary playback use plain HTTP requests, so they can keep working when WebSockets fail, but a client without a WebSocket connection never learns that it joined a group and does not follow the group's commands. A reverse proxy must therefore pass WebSocket upgrades. The official nginx example proxies the `/socket` location with the `Upgrade` and `Connection` headers; see jellyfin://guides/remote-access for proxy examples.

## Media Access

Every member must be able to see every item in the group's queue. The item's library must be enabled for the user, and the item must pass the user's parental controls (the maximum rating, blocking of unrated items, and allowed or blocked tags). Jellyfin leaves a group out of the group list of a user who cannot see its queue, refuses a join with "Access to this content is restricted.", and ignores a new queue that any member cannot see.

## Common Problems

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| No SyncPlay button | SyncPlay access is Disabled for this user, or the client does not support SyncPlay | Set SyncPlay access on the user's Profile tab, or use a supported client |
| "Permission required to create a group." or "No groups available" | The user can only join, and no group exists yet | Have a user with create access start the group, or raise this user's access |
| A group is missing from the list, or "Access to this content is restricted." | The user cannot see an item in the group's queue | Give the user access to that library, or adjust their parental controls |
| "Failed to join group because it does not exist." | The group closed; Jellyfin removes a group when its last member leaves | Create a new group |
| Everyone stays paused after a join, a seek, or a new item | A member is still loading or has stalled | Wait, press play to start without that member (twice if the group was paused), or ask them to choose Stop local playback or leave; lower their streaming quality if they buffer often |
| One member never follows play, pause, or seek | That client has no working WebSocket connection, for example because a reverse proxy does not pass WebSocket upgrades | Pass WebSocket upgrades through the proxy (see Network Requirements) |
| "Playback permission required." | The browser blocked automatic media playback | Allow autoplay for the Jellyfin site in the browser settings, then open the SyncPlay menu again |
| Members drift apart during long playback | Sync Correction is off, which is the default | Turn on Sync Correction in the SyncPlay settings on the client that drifts |
| Heavy stuttering during group playback | Sync Correction keeps adjusting speed or position | Turn off Sync Correction in the SyncPlay settings on that client |

## Server Log Messages

SyncPlay log lines come from the SyncPlay manager, group, and group-state classes, so they contain "SyncPlay". Read them with `jellyfin_system_info action=logs` and `action=log_file`; the session IDs in them match the IDs from `jellyfin_sessions`.

- "Session ... created group ...", "Session ... joined group ...", and "Group ... switching from ... to ..." are informational and trace what the group did.
- "Session ... tried to join group ... that does not exist." is a warning for a join to a closed group.
- "Session ... tried to join group ... but does not have access to some content of the playing queue." is a warning for a media access mismatch.
- "Unable to set playing queue in group ..." and "Unable to add items to play queue in group ..." are errors for a queue change the group refused, for example because a member cannot see one of the items.
- "Session ... is not time syncing properly. Ignoring elapsed time." is a warning that a client reported a time too far from the server's clock, so the server ignored the time elapsed since that report.
- "Group ... waited ... ms for session(s) ... to report ready, giving up." is a warning from Jellyfin 12.1 that names the sessions that held up the group. Jellyfin 10.11 and 12.0 have no wait limit and never log it.

## Why This Server Cannot Run SyncPlay

This server connects to Jellyfin with an API key. Jellyfin authorizes every SyncPlay request by the user attached to the token, and an API key carries no user, so Jellyfin rejects SyncPlay requests from this server. Setting JELLYFIN_USER_ID does not change that, because it only chooses the user for per-user queries. Even with a user token, the server would join as a member without a player that never reports ready, which would hold up playback for everyone else. This server therefore helps with SyncPlay by checking sessions, devices, user access, and logs, while the people watching create and join groups in their own clients.

## Sources

- https://jellyfin.org/posts/jellyfin-10-6-0/
- https://jellyfin.org/docs/general/post-install/networking/reverse-proxy/nginx/
- https://github.com/jellyfin/jellyfin (Emby.Server.Implementations/SyncPlay, Jellyfin.Api/Auth/SyncPlayAccessPolicy)
- https://github.com/jellyfin/jellyfin-web (src/plugins/syncPlay)
- https://github.com/jellyfin/jellyfin-desktop
- https://github.com/jellyfin/jellyfin-android
