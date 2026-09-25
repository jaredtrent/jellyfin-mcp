# Users and Access Control

Create accounts, restrict each one to its own libraries, and set parental controls for children's accounts.

Every person who uses the server gets their own account, which keeps watch history, favorites, and resume positions separate. An account's policy decides what it can see and do. `jellyfin_users action=create` creates an account, and `action=update_policy` changes its policy.

## Roles

- **Administrator**: Reaches the dashboard, every setting, every user, and every library.
- **User**: Plays media from the libraries the policy grants.
- **Disabled**: The account exists, but it can't sign in.

The dashboard groups a user's settings into four tabs: Profile, Library Access, Parental Control, and Password.

## Library Access

Turn off access to all libraries and list the ones the account may see:

~~~
jellyfin_users action=update_policy user_id=<id>
  enable_all_folders=false
  enabled_folder_ids=["<library-id>", "<library-id>"]
~~~

`jellyfin_libraries` lists the library IDs. An account without access to all libraries doesn't receive a library you add later, so grant it when you create the library. On the same tab, turning off access from all devices makes an administrator approve each new device the account signs in from.

## Parental Controls

Set these on the account's Parental Control tab in Dashboard > Users. The tools don't set them.

- **Maximum allowed parental rating**: Items rated above this limit, such as anything above PG-13, are hidden from the account.
- **Block items with no or unrecognized rating information**: Hides items that have no rating, which otherwise pass the limit above.
- **Block items with tags**: Hides any item that carries one of the listed tags. Add tags such as `violence` to items with `jellyfin_metadata`, then list them here.
- **Allow items with tags**: Shows only items that carry one of the listed tags.
- **Access Schedule**: Limits sign-in to the listed days and hours. Playback stops when the window closes.

## Failed Logins

Jellyfin locks an account after repeated failed logins: three attempts for a user and five for an administrator, unless the policy sets its own threshold, and a threshold of -1 turns the lock off. An administrator unlocks the account by clearing Disable this user under Additional options and saving. When the locked account is the only administrator, see jellyfin://guides/troubleshooting for the database fix.

## Invitations

jfa-go is a third-party account manager that hands out invitation links, so people create their own accounts with a policy you chose in advance instead of asking you for a password.

## Sources

- https://jellyfin.org/docs/general/server/users/adding-managing-users/
