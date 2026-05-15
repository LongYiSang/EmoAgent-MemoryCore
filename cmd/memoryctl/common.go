package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

const (
	formatText = "text"
	formatJSON = "json"
	formatID   = "id"
	formatTSV  = "tsv"
)

type commonOptions struct {
	DBPath      string
	PersonaID   string
	Format      string
	AutoMigrate bool
	EnableFTS   bool
	Pretty      bool
	ConfigPath  string
}

type stringList []string

func (l *stringList) String() string {
	return strings.Join(*l, ",")
}

func (l *stringList) Set(value string) error {
	*l = append(*l, value)
	return nil
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func addCommonFlags(fs *flag.FlagSet, opts *commonOptions, defaultFormat string) {
	fs.StringVar(&opts.DBPath, "db", "", "SQLite database path")
	fs.StringVar(&opts.PersonaID, "persona", "default", "persona id")
	fs.StringVar(&opts.Format, "format", defaultFormat, "output format")
	fs.BoolVar(&opts.AutoMigrate, "auto-migrate", false, "apply migrations before opening service")
	fs.BoolVar(&opts.EnableFTS, "enable-fts", true, "enable optional FTS migrations when migrations run")
	fs.BoolVar(&opts.Pretty, "pretty", false, "pretty-print JSON output")
}

func parseFlags(fs *flag.FlagSet, args []string) bool {
	return fs.Parse(args) == nil
}

func requireDB(stderr io.Writer, fs *flag.FlagSet, dbPath string) bool {
	if strings.TrimSpace(dbPath) != "" {
		return true
	}
	fmt.Fprintln(stderr, "--db is required")
	fs.Usage()
	return false
}

func validateFormat(format string, allowed ...string) error {
	for _, value := range allowed {
		if format == value {
			return nil
		}
	}
	return fmt.Errorf("--format %s is not supported for this command", format)
}

func openService(ctx context.Context, opts commonOptions) (memorycore.Service, error) {
	return memorycore.Open(ctx, memorycore.Options{
		DBPath:      opts.DBPath,
		PersonaID:   opts.PersonaID,
		AutoMigrate: opts.AutoMigrate,
		EnableFTS:   opts.EnableFTS,
	})
}

func writeJSON(stdout io.Writer, value any, pretty bool) int {
	var data []byte
	var err error
	if pretty {
		data, err = json.MarshalIndent(value, "", "  ")
	} else {
		data, err = json.Marshal(value)
	}
	if err != nil {
		fmt.Fprintf(stdout, `{"error":%q}`+"\n", err.Error())
		return 1
	}
	fmt.Fprintln(stdout, string(data))
	return 0
}

func stringPtr(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func readTextValue(value string, filePath string) (string, error) {
	hasValue := strings.TrimSpace(value) != ""
	hasFile := strings.TrimSpace(filePath) != ""
	if hasValue == hasFile {
		return "", errors.New("exactly one inline value or file path must be provided")
	}
	if hasValue {
		return value, nil
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func readOptionalTextValue(value string, filePath string) (*string, error) {
	if strings.TrimSpace(value) == "" && strings.TrimSpace(filePath) == "" {
		return nil, nil
	}
	text, err := readTextValue(value, filePath)
	if err != nil {
		return nil, err
	}
	return &text, nil
}

func readInputFile(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func parseOptionalTime(value string, flagName string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be RFC3339: %w", flagName, err)
	}
	return parsed, nil
}

func parseOptionalTimePtr(value string, flagName string) (*time.Time, error) {
	parsed, err := parseOptionalTime(value, flagName)
	if err != nil || parsed.IsZero() {
		return nil, err
	}
	return &parsed, nil
}

func validateOneOf(name string, value string, allowed ...string) error {
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of %s", name, strings.Join(allowed, "|"))
}

func validateFloatRange(name string, value float64, min float64, max float64) error {
	if value < min || value > max {
		return fmt.Errorf("%s must be between %s and %s", name, trimFloat(min), trimFloat(max))
	}
	return nil
}

func trimFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func usageError(stderr io.Writer, fs *flag.FlagSet, message string, args ...any) int {
	fmt.Fprintf(stderr, message+"\n", args...)
	if fs != nil {
		fs.Usage()
	}
	return 2
}

func runtimeError(stderr io.Writer, message string, args ...any) int {
	fmt.Fprintf(stderr, message+"\n", args...)
	return 1
}

func idOutput(stdout io.Writer, id string) int {
	fmt.Fprintln(stdout, id)
	return 0
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func sensitivityRank(level string) int {
	switch level {
	case memorycore.SensitivityHighlySensitive:
		return 2
	case memorycore.SensitivitySensitive:
		return 1
	default:
		return 0
	}
}
