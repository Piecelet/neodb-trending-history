package trending

import (
    "bufio"
    "context"
    "errors"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "regexp"
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
    ts := now.Unix()

    for _, host := range hosts {
        dash := dashifyHost(host)
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
                fname := fmt.Sprintf("%d-%s-%s.json", ts, dash, t)
                fpath := filepath.Join(dir, fname)
                if writeErr := os.WriteFile(fpath, data, 0o644); writeErr != nil {
                    logf("ERR write %s: %v", fpath, writeErr)
                    return
                }
                logf("OK  saved %s", fpath)
            }()
        }
    }

    return nil
}
