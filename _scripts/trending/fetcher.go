package trending

import (
    "bufio"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "regexp"
    "sort"
    "strings"
    "time"
)

// Types contains the supported trending categories.
var Types = []string{"book", "movie", "tv", "music", "game", "podcast", "collection"}

// Config holds runtime parameters.
type Config struct {
    InstancesFile string
    OutputRoot    string
    Types         []string
    HTTPTimeout   time.Duration
    UserAgent     string
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
    return Config{
        InstancesFile: filepath.Join("_config", "instance.txt"),
        OutputRoot:    ".",
        Types:         append([]string{}, Types...),
        HTTPTimeout:   20 * time.Second,
        UserAgent:     "neodb-trending-history-bot",
    }
}

var hostAllowed = regexp.MustCompile(`[a-z0-9.-]+(:[0-9]+)?`)

// sanitizeHost ensures the host is lowercase and safe; returns error if invalid.
func sanitizeHost(in string) (string, error) {
    h := strings.TrimSpace(strings.ToLower(in))
    if h == "" {
        return "", errors.New("empty host")
    }
    if strings.Contains(h, "/") || strings.Contains(h, " ") || strings.Contains(h, "http") {
        return "", fmt.Errorf("invalid host (use domain only): %q", in)
    }
    if !hostAllowed.MatchString(h) {
        return "", fmt.Errorf("invalid characters in host: %q", in)
    }
    return h, nil
}

// dashifyHost replaces separators to match required path format.
func dashifyHost(host string) string {
    // Replace dots and colons with dashes, collapse runs of non [a-z0-9-] into '-'
    s := strings.ReplaceAll(host, ".", "-")
    s = strings.ReplaceAll(s, ":", "-")
    // keep a-z0-9- only
    var b strings.Builder
    for _, r := range s {
        if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
            b.WriteRune(r)
        } else {
            b.WriteByte('-')
        }
    }
    out := b.String()
    out = strings.Trim(out, "-")
    out = strings.ReplaceAll(out, "--", "-")
    return out
}

// readInstances reads instances file, one host per line, ignoring blanks and comments.
func readInstances(path string) ([]string, error) {
    f, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer f.Close()
    var hosts []string
    s := bufio.NewScanner(f)
    for s.Scan() {
        line := strings.TrimSpace(s.Text())
        if line == "" || strings.HasPrefix(line, "#") {
            continue
        }
        h, err := sanitizeHost(line)
        if err != nil {
            return nil, err
        }
        hosts = append(hosts, h)
    }
    if err := s.Err(); err != nil {
        return nil, err
    }
    return hosts, nil
}

// FetchAll fetches trending data for each instance and type, writing JSON files.
func FetchAll(cfg Config, logf func(format string, args ...any)) error {
    if logf == nil {
        logf = func(string, ...any) {}
    }
    hosts, err := readInstances(cfg.InstancesFile)
    if err != nil {
        return fmt.Errorf("read instances: %w", err)
    }
    if len(hosts) == 0 {
        logf("no instances configured in %s", cfg.InstancesFile)
        return nil
    }

    client := &http.Client{Timeout: cfg.HTTPTimeout}
    now := time.Now().UTC()
    y, m, d := now.Date()
    ts := now.Format(time.RFC3339Nano)

    for _, host := range hosts {
        dash := dashifyHost(host)
        // Collect per-type raw payloads and simplified entries for summary/README.
        typePayloads := make(map[string]json.RawMessage, len(cfg.Types))
        typeEntries := make(map[string][]entry, len(cfg.Types))
        for _, t := range cfg.Types {
            url := fmt.Sprintf("https://%s/api/trending/%s/", host, t)
            req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
            if err != nil {
                logf("ERR build request %s %s: %v", host, t, err)
                continue
            }
            req.Header.Set("Accept", "application/json")
            if cfg.UserAgent != "" {
                req.Header.Set("User-Agent", cfg.UserAgent)
            }

            resp, err := client.Do(req)
            if err != nil {
                logf("ERR fetch %s %s: %v", host, t, err)
                continue
            }
            func() {
                defer resp.Body.Close()
                if resp.StatusCode != http.StatusOK {
                    logf("WARN non-200 %s %s: %s", host, t, resp.Status)
                    return
                }

                // Limit read to 10MB to avoid bad responses.
                const maxBytes = 10 << 20
                r := io.LimitReader(resp.Body, maxBytes)
                data, err := io.ReadAll(r)
                if err != nil {
                    logf("ERR read body %s %s: %v", host, t, err)
                    return
                }

                dir := filepath.Join(cfg.OutputRoot, dash, fmt.Sprintf("%04d", y), fmt.Sprintf("%02d", int(m)), fmt.Sprintf("%02d", d))
                if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
                    logf("ERR mkdir %s: %v", dir, mkErr)
                    return
                }
                fname := fmt.Sprintf("%s-%s-%s.json", ts, dash, t)
                fpath := filepath.Join(dir, fname)
                if writeErr := os.WriteFile(fpath, data, 0o644); writeErr != nil {
                    logf("ERR write %s: %v", fpath, writeErr)
                    return
                }
                logf("OK  saved %s", fpath)

                // Save for summary and README.
                typePayloads[t] = json.RawMessage(data)
                ents := toEntries(data, host, t)
                if len(ents) > 0 {
                    typeEntries[t] = ents
                }
            }()
        }

        // Write summary JSON without trailing type in filename.
        if len(typePayloads) > 0 {
            if err := writeSummaryJSON(cfg.OutputRoot, dash, y, int(m), d, ts, host, typePayloads); err != nil {
                logf("ERR write summary for %s: %v", host, err)
            }
        }

        // Append README section with a table snapshot.
        if len(typeEntries) > 0 {
            if err := appendREADME(cfg.OutputRoot, dash, host, y, int(m), d, ts, typeEntries); err != nil {
                logf("ERR write README for %s: %v", host, err)
            }
        }
    }

    return nil
}

// entry is a simplified view for README rendering.
type entry struct {
    Title string
    Image string
    Link  string
}

// toEntries attempts to parse trending payload into a list of entries.
func toEntries(data []byte, host string, typ string) []entry {
    var v any
    if err := json.Unmarshal(data, &v); err != nil {
        return nil
    }
    // find array
    var arr []any
    switch vv := v.(type) {
    case []any:
        arr = vv
    case map[string]any:
        // common keys holding arrays
        for _, k := range []string{"results", "items", "data", "objects", "list"} {
            if x, ok := vv[k]; ok {
                if xs, ok := x.([]any); ok {
                    arr = xs
                    break
                }
            }
        }
        // some APIs may have map of type->array; flatten first array
        if arr == nil {
            // try any first array value
            keys := make([]string, 0, len(vv))
            for k := range vv {
                keys = append(keys, k)
            }
            sort.Strings(keys)
            for _, k := range keys {
                if xs, ok := vv[k].([]any); ok {
                    arr = xs
                    break
                }
            }
        }
    default:
        return nil
    }
    if arr == nil {
        return nil
    }
    out := make([]entry, 0, len(arr))
    for _, it := range arr {
        if m, ok := it.(map[string]any); ok {
            title := pickTitle(m)
            img := pickImage(m)
            link := pickLink(m, host, typ)
            if title == "" && img == "" && link == "" {
                continue
            }
            out = append(out, entry{Title: title, Image: img, Link: link})
        }
    }
    return out
}

func pickTitle(m map[string]any) string {
    // direct keys
    for _, k := range []string{"title", "name", "text", "caption"} {
        if v, ok := m[k]; ok {
            if s, ok := v.(string); ok && s != "" {
                return s
            }
        }
    }
    // nested subject
    if sub, ok := m["subject"].(map[string]any); ok {
        for _, k := range []string{"title", "name"} {
            if v, ok := sub[k]; ok {
                if s, ok := v.(string); ok && s != "" {
                    return s
                }
            }
        }
    }
    return ""
}

func pickImage(m map[string]any) string {
    // Prefer cover_image_url at top-level or under subject
    if v, ok := m["cover_image_url"].(string); ok && v != "" {
        return v
    }
    if sub, ok := m["subject"].(map[string]any); ok {
        if v, ok := sub["cover_image_url"].(string); ok && v != "" {
            return v
        }
    }
    // Fallback to other common keys just in case
    if v, ok := m["cover"].(string); ok && v != "" {
        return v
    }
    if v, ok := m["image"].(string); ok && v != "" {
        return v
    }
    if sub, ok := m["subject"].(map[string]any); ok {
        if v, ok := sub["cover"].(string); ok && v != "" {
            return v
        }
        if v, ok := sub["image"].(string); ok && v != "" {
            return v
        }
    }
    return ""
}

func pickLink(m map[string]any, host, typ string) string {
    // Prefer explicit URL fields
    if v, ok := m["url"].(string); ok && v != "" {
        if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
            return v
        }
        if strings.HasPrefix(v, "/") {
            return "https://" + host + v
        }
        return "https://" + host + "/" + v
    }
    if sub, ok := m["subject"].(map[string]any); ok {
        if v, ok := sub["url"].(string); ok && v != "" {
            if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
                return v
            }
            if strings.HasPrefix(v, "/") {
                return "https://" + host + v
            }
            return "https://" + host + "/" + v
        }
    }
    // Construct from id if available
    if v, ok := m["id"].(string); ok && v != "" {
        if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
            return v
        }
        if strings.HasPrefix(v, "/") {
            return "https://" + host + v
        }
        return fmt.Sprintf("https://%s/%s/%s", host, typ, v)
    }
    if sub, ok := m["subject"].(map[string]any); ok {
        if v, ok := sub["id"].(string); ok && v != "" {
            if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
                return v
            }
            if strings.HasPrefix(v, "/") {
                return "https://" + host + v
            }
            return fmt.Sprintf("https://%s/%s/%s", host, typ, v)
        }
    }
    return ""
}

func writeSummaryJSON(root, dash string, y int, m int, d int, ts string, host string, payloads map[string]json.RawMessage) error {
    dir := filepath.Join(root, dash, fmt.Sprintf("%04d", y), fmt.Sprintf("%02d", m), fmt.Sprintf("%02d", d))
    if err := os.MkdirAll(dir, 0o755); err != nil {
        return err
    }
    fname := fmt.Sprintf("%s-%s.json", ts, dash)
    fpath := filepath.Join(dir, fname)
    obj := map[string]any{
        "timestamp": ts,
        "host":      host,
        "types":     payloads,
    }
    data, err := json.MarshalIndent(obj, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(fpath, data, 0o644)
}

func appendREADME(root, dash, host string, y int, m int, d int, ts string, typeEntries map[string][]entry) error {
    dir := filepath.Join(root, dash, fmt.Sprintf("%04d", y), fmt.Sprintf("%02d", m), fmt.Sprintf("%02d", d))
    if err := os.MkdirAll(dir, 0o755); err != nil {
        return err
    }
    fpath := filepath.Join(dir, "README.md")

    var b strings.Builder
    if _, err := os.Stat(fpath); errors.Is(err, os.ErrNotExist) {
        // New file: add top-level title
        b.WriteString(fmt.Sprintf("# NeoDB Trending History for %s\n\n", host))
    }
    b.WriteString(fmt.Sprintf("## %s\n", ts))
    // Build a wide table: first column is row label, remaining 19 cells for items
    itemCols := 19
    totalCols := 1 + itemCols
    // header row with blanks
    b.WriteString("|")
    for i := 0; i < totalCols; i++ {
        b.WriteString("      |")
    }
    b.WriteString("\n|")
    for i := 0; i < totalCols; i++ {
        b.WriteString(" ---- |")
    }
    b.WriteString("\n")
    // rows: include only types that were fetched (non-empty entries), keep stable order
    for _, t := range Types {
        ents, ok := typeEntries[t]
        if !ok || len(ents) == 0 {
            continue
        }
        label := typeLabel(t)
        cells := renderCells(ents, itemCols, host, t)
        // write row label + cells
        b.WriteString("|")
        b.WriteString(" ")
        b.WriteString(escapePipes(label))
        b.WriteString(" |")
        for _, c := range cells {
            b.WriteString(" ")
            b.WriteString(c)
            b.WriteString(" |")
        }
        b.WriteString("\n")
    }
    b.WriteString("\n")

    // Append to file
    f, err := os.OpenFile(fpath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
    if err != nil {
        return err
    }
    defer f.Close()
    _, err = f.WriteString(b.String())
    return err
}

func renderCells(ents []entry, columns int, host, typ string) []string {
    cells := make([]string, columns)
    for i := 0; i < columns; i++ {
        if i < len(ents) {
            t := strings.TrimSpace(ents[i].Title)
            t = escapePipes(t)
            img := ents[i].Image
            if strings.HasPrefix(img, "/") {
                img = "https://" + host + img
            }
            link := ents[i].Link
            if link != "" {
                // Make both image and title clickable as one link
                if img != "" {
                    cells[i] = fmt.Sprintf("[![](%s)<br/>%s](%s)", img, t, link)
                } else {
                    cells[i] = fmt.Sprintf("[%s](%s)", t, link)
                }
            } else {
                if img != "" {
                    cells[i] = fmt.Sprintf("![](%s)<br/>%s", img, t)
                } else {
                    cells[i] = t
                }
            }
        } else {
            cells[i] = ""
        }
    }
    return cells
}

func typeLabel(t string) string {
    switch t {
    case "book":
        return "books"
    case "movie":
        return "movies"
    case "tv":
        return "tv"
    case "music":
        return "music"
    case "game":
        return "games"
    case "podcast":
        return "podcasts"
    case "collection":
        return "collections"
    default:
        return t
    }
}

func escapePipes(s string) string {
    // Escape '|' to avoid breaking table cells
    return strings.ReplaceAll(s, "|", "\\|")
}
