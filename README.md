# paprika-3-mcp

A [Model Context Protocol (MCP)](https://modelcontextprotocol.io/introduction) server that connects your **Paprika 3** account to LLMs like Claude — manage recipes, plan meals, and build grocery lists through natural conversation.

### 🖼️ Example: Claude using the Paprika MCP server

<p align="center">
  <img src="docs/example.png" alt="MCP server running with Claude" />
</p>

## 🚀 Features

See anything missing? Open an issue on this repo to request a feature!

#### 📄 **Resources**

- Recipes ✅
- Recipe Photos 🚧

#### 🛠 **Tools**

**Recipe Management**
- `create_paprika_recipe` — Create new recipes in your Paprika app
- `update_paprika_recipe` — Modify existing recipes by UID
- `get_recipe` — Get full recipe details (ingredients, directions, etc.) by UID
- `list_recipes` — List all recipes with names, UIDs, and basic info (fast, parallel fetch)
- `search_recipes` — Deep search recipes by keyword across name, ingredients, and description
- `delete_recipe` — Move a recipe to the trash by UID (soft delete; recoverable in the Paprika app)

**Meal Planning**
- `list_meal_plan` — View scheduled meals, optionally filtered by date range
- `add_meal_to_plan` — Schedule a meal (Breakfast, Lunch, or Dinner) with an optional recipe link
- `remove_meal_from_plan` — Remove a meal from the plan by UID

**Grocery Management**
- `list_grocery_lists` — View all grocery lists (Paprika supports multiple lists, e.g. one per store)
- `list_aisles` — View grocery store aisles (synced from Paprika Settings)
- `list_groceries` — View grocery items across all lists, grouped by aisle, with purchase status
- `add_grocery_item` — Add an item to a grocery list with automatic aisle assignment (history → form modifier → keyword table → Miscellaneous). The tool result always says which aisle was chosen and why.
- `remove_grocery_item` — Remove an item by name (case-insensitive partial match)

## ⚙️ Prerequisites

- ✅ A Mac, Linux, or Windows system
- ✅ [Paprika 3](https://www.paprikaapp.com/) installed with cloud sync enabled
- ✅ Your Paprika 3 **username and password**
- ✅ Claude or any LLM client with **MCP tool support** enabled

## 🛠 Installation

You can download a prebuilt binary from the [Releases](https://github.com/jimternet/paprika-3-mcp/releases) page.

### 🍎 macOS (via Homebrew)

If you're on macOS, the easiest way to install is with [Homebrew](https://brew.sh/):

```bash
brew tap jimternet/tap
brew install jimternet/tap/paprika-3-mcp
```

### 🐧 Linux / 🪟 Windows

1. Go to the [latest release](https://github.com/soggycactus/paprika-3-mcp/releases).
2. Download the appropriate archive for your operating system and architecture:
   - `paprika-3-mcp_<version>_linux_amd64.zip` for Linux
   - `paprika-3-mcp_<version>_windows_amd64.zip` for Windows
3. Extract the zip archive:
   - **Linux**:
     ```bash
     unzip paprika-3-mcp_<version>_<os>_<arch>.zip
     ```
   - **Windows**:
     - Right-click the `.zip` file and select **Extract All**, or use a tool like 7-Zip.
4. Move the binary to a directory in your system's `$PATH`:

   - Linux:

     ```bash
     sudo mv paprika-3-mcp /usr/local/bin/
     ```

   - Windows:
     - Move `paprika-3-mcp.exe` to any folder in your `PATH` (e.g., `%USERPROFILE%\bin`)

### ✅ Test the installation

You can verify the server is installed by checking:

```bash
paprika-3-mcp --version
```

You should see:

```bash
paprika-3-mcp version v0.1.0
```

## 🤖 Setting up Claude

If you haven't setup MCP before, [first read more about how to install Claude Desktop client & configure an MCP server.](https://modelcontextprotocol.io/quickstart/user)

To add `paprika-3-mcp` to Claude, all you need to do is create another entry in the `mcpServers` section of your `claude_desktop_config.json` file.

### Option 1: macOS Keychain (recommended — no credentials in config)

Store your password in the macOS Keychain once:

```bash
security add-generic-password -s paprika-3-mcp -a your@email.com -w
```

Then configure Claude with only your username in the environment — no password in the file at all:

```json
{
  "mcpServers": {
    "paprika-3": {
      "command": "paprika-3-mcp",
      "env": {
        "PAPRIKA_USERNAME": "your@email.com"
      }
    }
  }
}
```

At startup the server looks up the password from the Keychain automatically using the username as the account key.

### Option 2: Environment variables

```json
{
  "mcpServers": {
    "paprika-3": {
      "command": "paprika-3-mcp",
      "env": {
        "PAPRIKA_USERNAME": "your@email.com",
        "PAPRIKA_PASSWORD": "your-password"
      }
    }
  }
}
```

### Option 3: CLI flags (least preferred — credentials visible in process list)

```json
{
  "mcpServers": {
    "paprika-3": {
      "command": "paprika-3-mcp",
      "args": [
        "--username",
        "<your paprika 3 username (usually email)>",
        "--password",
        "<your paprika 3 password>"
      ]
    }
  }
}
```

Restart Claude and you should see the MCP server tools after clicking on the hammerhead icon:

![MCP server running with Claude](docs/install.png)

## 🛒 Grocery aisles

### How aisle assignment works

Paprika does not assign aisles server-side — items POSTed to the API without an aisle land in Miscellaneous. This server assigns aisles locally before sending the item. For every `add_grocery_item` call it tries four stages in order, stopping at the first match:

1. **History** — your Paprika account's learned ingredient-to-aisle table (synced on every refresh). If you've ever filed "kale" under Produce in the app, the next `add_grocery_item("kale")` uses that.
2. **Modifier** — a form word in the raw input maps directly to an aisle. "canned corn" fires the `canned` modifier → Canned and Jar Goods; "frozen pizza" fires `frozen` → Frozen Foods. Whole-word only, so "cannellini beans" never matches `can`.
3. **Keyword** — a built-in table of ~250 ingredients targeting Paprika's stock aisle names. Longest keyword wins, so "corn tortillas" → Breads and Cereals, not "corn" → Produce.
4. **No match** — aisle is left empty; Paprika files it as Miscellaneous.

The tool result and the server log both report which aisle was chosen and which stage found it. Re-filing an item in the Paprika app updates your history and overrides the table on the next refresh.

### Fixing a mis-filed item

1. Test the current resolution offline:
   ```bash
   paprika-3-mcp aisles test "canned corn"
   ```
   The output shows the stage (`history`, `modifier`, `keyword`, or `no match`), the chosen aisle, and the aisle UID.

2. If the stage is `keyword` or `no match`, add an override to your aisles config:
   ```
   ~/Library/Application Support/paprika-3-mcp/aisles.yaml   # macOS
   ~/.cache/paprika-3-mcp/aisles.yaml                         # Linux
   ```
   Add the keyword under the correct aisle:
   ```yaml
   keywords:
     Produce:
       - canned corn   # moves this keyword from Canned and Jar Goods to Produce
   ```
   Run `aisles test` again to confirm. No restart needed — the server picks up the change on the next background refresh.

3. If the stage is `history`, the fix is in the Paprika app: drag the item to the correct aisle once. The corrected mapping syncs on the next refresh.

### Adding or renaming aisles

1. Rename or add the aisle in Paprika (Settings → Grocery Aisles).
2. Generate a starter config with your live aisle names:
   ```bash
   paprika-3-mcp aisles export           # writes aisles.yaml (fails if file exists)
   paprika-3-mcp aisles export --force   # overwrites an existing file
   ```
3. Edit `aisles.yaml` to update any modifier or keyword targets that still reference the old name.
4. Run `paprika-3-mcp aisles validate` to check for broken references (exits 1 if any found).

### Reference

**Config file location**

| OS | Default path |
|----|-------------|
| macOS | `~/Library/Application Support/paprika-3-mcp/aisles.yaml` |
| Linux | `~/.cache/paprika-3-mcp/aisles.yaml` |
| Windows | `%LOCALAPPDATA%\paprika-3-mcp\aisles.yaml` |

Override with `--aisles-config <path>` (server) or `PAPRIKA_AISLES_CONFIG` (env var). The env var also works for the `aisles` subcommands.

**Config overlay rules**

The embedded defaults target Paprika's 24 stock aisle names. Your config file is merged on top:
- `aisles:` — your list **replaces** the default list entirely (used for validation).
- `modifiers:` — your entries **merge over** the defaults, key by key.
- `keywords:` — your entries **merge per aisle**. Listing a keyword under aisle X removes it from every other aisle, so one entry is enough to move it.

**Modifiers**

`canned`, `can`, `cans`, `tin`, `tinned`, `jar`, `jarred` → Canned and Jar Goods  
`frozen` → Frozen Foods  
`dried`, `dry` → Pasta, Rice and Beans

Matching is whole-word and case-insensitive on the raw ingredient string (before quantity stripping).

**Subcommands**

```bash
# Test any ingredient string offline
paprika-3-mcp aisles test "1 (15 oz) can chickpeas"

# Write a starter aisles.yaml with your live Paprika aisle names
paprika-3-mcp aisles export [--force]

# Validate your aisles.yaml — exits 1 if any aisle is undefined
paprika-3-mcp aisles validate [--aisles-config <path>]
```

**Log lines to watch**

Look for `aisle=` and `stage=` in the server log (macOS: `~/Library/Logs/paprika-3-mcp/server.log`). The `aisles config loaded` INFO line at startup shows which file was used, the aisle count, and the keyword count.

## 🙏 Acknowledgements

This project is a fork of [soggycactus/paprika-3-mcp](https://github.com/soggycactus/paprika-3-mcp), created by [Lucas Stephens](https://github.com/soggycactus). A huge thank you to Lucas for the original work and to the community contributors — [JoshTerAvest](https://github.com/JoshTerAvest), [okhick](https://github.com/okhick), and [bsitkoff](https://github.com/bsitkoff) — whose pull requests formed the foundation of this fork. None of this would exist without their effort.

## 📄 License

This project is open source under the [MIT License](./LICENSE) © 2025 [Lucas Stephens](https://github.com/soggycactus).

---

#### 🗂 Miscellaneous

##### 📄 Where can I see the server logs?

The MCP server writes structured logs using Go’s `slog` with rotation via `lumberjack`. Log files are automatically created based on your operating system:

| Operating System | Log File Path                             |
| ---------------- | ----------------------------------------- |
| macOS            | `~/Library/Logs/paprika-3-mcp/server.log` |
| Linux            | `/var/log/paprika-3-mcp/server.log`       |
| Windows          | `%APPDATA%\paprika-3-mcp\server.log`      |
| Other / Unknown  | `/tmp/paprika-3-mcp/server.log`           |

> 💡 Logs are rotated automatically at 100MB, with only 5 backup files kept. Logs are also wiped after 10 days.
