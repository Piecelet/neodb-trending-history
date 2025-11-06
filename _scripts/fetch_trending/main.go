package main

import (
    "flag"
    "fmt"
    "os"
    "strings"
    "time"

    "neodb-trending-history/_scripts/trending"
)

func main() {
    cfg := trending.DefaultConfig()

    var typesCSV string
    flag.StringVar(&cfg.InstancesFile, "instances", cfg.InstancesFile, "path to instances.txt (one host per line)")
    flag.StringVar(&cfg.OutputRoot, "out", cfg.OutputRoot, "output root directory")
    flag.StringVar(&typesCSV, "types", strings.Join(cfg.Types, ","), "comma-separated trending types")
    flag.DurationVar(&cfg.HTTPTimeout, "timeout", cfg.HTTPTimeout, "HTTP timeout")
    flag.StringVar(&cfg.UserAgent, "ua", cfg.UserAgent, "HTTP User-Agent header")
    flag.Parse()

    if typesCSV != "" {
        parts := strings.Split(typesCSV, ",")
        cfg.Types = make([]string, 0, len(parts))
        for _, p := range parts {
            p = strings.TrimSpace(p)
            if p != "" {
                cfg.Types = append(cfg.Types, p)
            }
        }
    }

    started := time.Now()
    logf := func(format string, args ...any) {
        fmt.Fprintf(os.Stdout, format+"\n", args...)
    }

    if err := trending.FetchAll(cfg, logf); err != nil {
        fmt.Fprintf(os.Stderr, "fetch failed: %v\n", err)
        os.Exit(1)
    }
    logf("done in %s", time.Since(started).Truncate(time.Millisecond))
}
