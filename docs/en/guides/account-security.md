# Account Security

Manage login verification, devices, and one-time login links from **User Settings → Security**.

## Login and Devices

Security settings support changing passwords, two-factor authentication, recovery codes, login devices, and one-time login links. The login devices list only shows active sessions, with the current device pinned to the top. Device names use formats such as "Chrome / Windows · Web" and display the last login and access times. You can sign out of a single device or sign out of all other devices; background tasks are not automatically cancelled. The **Sign Out** button at the bottom of the Security page signs out only the current device, while other devices remain logged in.

Repeated logins from the same browser to the same service address update the original device session. After a normal sign-out, logging in again is still recognized as the same device. Different browsers, service addresses, and clients are independent; clearing site data results in recognition as a new device. Expired device sessions are immediately hidden from the list and automatically cleaned up after 7 days.

One-time links can have an expiration of 15 minutes, 1 hour, or 24 hours. The full link is displayed only once; used, expired, or revoked links cannot be redeemed. If two-factor authentication is enabled, a verification code is still required. To get a new link, regenerate it from a logged-in device. By default, available links and those awaiting two-factor authentication are shown, while used, revoked, or expired records are collapsed under "Recent Records." Expired links are automatically cleaned up after 7 days.

**Sensitive Operation Verification** is disabled by default. When enabled, saving/changing credentials and exporting data require re-verification. Changing passwords and security settings always require identity confirmation. If an account cannot be logged into, the deployer should follow the [Local Recovery Process](../deployment/maintenance.md#backup-recovery-and-upgrades).
