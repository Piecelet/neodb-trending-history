# neodb-trending-history

Fetch and store NeoDB trending history for configured instances.

## Structure

- `_config/instance.txt`: one instance domain per line (no scheme).
- `_scripts/fetch_trending`: Go CLI entry.
- `_scripts/trending`: shared code for fetching and storage.
- Output path: `{instance_host_dash}/{yyyy}/{mm}/{dd}/{timestamp-instance_host_dash-trending_type}.json`
  - Example: `neodb-social/2025/11/06/1730937600-neodb-social-book.json`

## Trending endpoints

- `/api/trending/book/`
- `/api/trending/movie/`
- `/api/trending/tv/`
- `/api/trending/music/`
- `/api/trending/game/`
- `/api/trending/podcast/`
- `/api/trending/collection/`

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
