# Discord Application Setup

This guide walks you through creating a Discord Application to obtain a Client ID for discord-proxy-rpc.

## Prerequisites

- A Discord account
- Access to the [Discord Developer Portal](https://discord.com/developers/applications)

## Step-by-Step

### 1. Create an Application

1. Go to [discord.com/developers/applications](https://discord.com/developers/applications) and log in.
2. Click **New Application** in the top-right corner.
3. Give it a name (e.g. "LAN Presence") and click **Create**.

### 2. Copy the Client ID

1. In the left sidebar, click **General Information**.
2. Copy the **Application ID** -- this is your Client ID.

> You do **not** need to enable any OAuth scopes or create a bot user. The proxy communicates with Discord over the local IPC socket, which only requires a valid Client ID.

### 3. Set the Client ID in discord-proxy-rpc

**Option A -- Config file** (recommended):

```yaml
discord:
  client_id: "YOUR_CLIENT_ID_HERE"
```

**Option B -- Environment variable:**

```bash
export PROXY_DISCORD_CLIENT_ID="YOUR_CLIENT_ID_HERE"
```

### 4. (Optional) Upload Rich Presence Art Assets

If you want to show custom icons in your presence:

1. In the Developer Portal, go to your application.
2. Click **Rich Presence** in the left sidebar, then **Art Assets**.
3. Upload images and note their **asset key** (the name you give each upload).
4. Reference the asset key in your presence data via `large_image` or `small_image`.

Example:

```json
{
  "assets": {
    "large_image": "my-game-logo",
    "large_text": "My Game",
    "small_image": "class-warrior",
    "small_text": "Warrior"
  }
}
```

## Troubleshooting

| Problem | Solution |
|---------|----------|
| "Invalid Client ID" error | Double-check the Client ID in your config matches the Application ID in the Developer Portal. |
| Presence not showing on other devices | Ensure Discord desktop is running and logged in on the same machine as the proxy. |
| Custom images not appearing | Verify the asset key name matches exactly (case-sensitive). Assets may take a few minutes to propagate. |
