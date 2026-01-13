# NeoDB Trending History

Fetch and store NeoDB trending history for configured instances.

## Structure

- `_config/instance.txt`: one instance domain per line (no scheme).
- `_scripts/fetch_trending`: Go CLI entry.
- `_scripts/trending`: shared code for fetching and storage.
- Per-type JSON snapshot: `{instance_host_dash}/{yyyy}/{mm}/{dd}/{time}/{timestamp-instance_host_dash-trending_type}.json`
  - `timestamp` uses RFC3339 (serverdate), e.g. `YYYY-MM-DDThh:mm:ss.sssZ` (may include more fractional digits).
  - `time` is the time-of-day subfolder like `hh:mm:ss.sssZ`.
  - Example: `neodb-social/2025/11/06/12:34:56.789Z/2025-11-06T12:34:56.789Z-neodb-social-trending-book.json`
- Summary JSON (no type suffix): `{instance_host_dash}/{yyyy}/{mm}/{dd}/{time}/{timestamp-instance_host_dash-trending}.json`
  - Structure: `{ "timestamp": string, "host": string, "types": { <type>: <raw_api_payload>, ... } }`
- Per-day README: `{instance_host_dash}/{yyyy}/{mm}/{dd}/README.md`
  - First time created, header contains:
    - `# NeoDB Trending History for [{host}](https://{host}/)`
    - `YYYY-MM-DD | [NeoDB Trending History by Piecelet](https://github.com/Piecelet/neodb-trending-history)`
  - Each fetch appends a section:
    - `## {RFC3339 timestamp}`
    - A 20-column table snapshot
      - Column 1: row label (books, movies, tv, music, games, podcasts, collections)
      - Columns 2–20: items for that row
      - Only rows for types fetched in that run are included (no empty rows)
      - Each cell: clickable image + title in one link; image alt equals title
        - With link: `[![{title}](img)<br/>{title}](url)`
        - No link: `![{title}](img)<br/>{title}` or just `{title}`
        - Relative image URLs are prefixed with `https://{host}`
      - Image source: `cover_image_url` (top-level) or `subject.cover_image_url` as fallback
      - Link source: prefer `url`; else build from `id` as `https://{host}/{type}/{id}`

## Trending endpoints

- `/api/trending/book/`
- `/api/trending/movie/`
- `/api/trending/tv/`
- `/api/trending/music/`
- `/api/trending/game/`
- `/api/trending/podcast/`
- `/api/trending/collection/`
- `/api/trending/performance/`
- `/api/trending/tag/`

## Local usage

1. Edit `_config/instance.txt` and add instance domains (e.g. `neodb.social`).
2. Run the fetcher:

   ```bash
   go run ./_scripts/fetch_trending -instances _config/instance.txt -out .
   ```

Flags:

- `-types`: comma-separated list of types (default: all supported)
- `-timeout`: HTTP timeout (default: 20s)
- `-ua`: custom User-Agent

## GitHub Actions

The workflow `.github/workflows/fetch.yml` runs every 4 hours and commits new files.
