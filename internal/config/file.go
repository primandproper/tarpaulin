package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/primandproper/platform-go/v10/encoding"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
)

// FileStem is the name a project's config file has, before its extension. The
// leading dot is the convention every other tool in a Go repository's root
// follows, and it keeps `ls` readable in a directory that already has a dozen
// of them.
const FileStem = ".tarp"

// goModFile is the file whose presence marks a module root, which is where
// discovery stops looking upwards.
const goModFile = "go.mod"

// ErrAmbiguousConfigFile is returned when one directory holds more than one
// .tarp file. Picking a winner by extension would mean a project could edit
// .tarp.yaml for an afternoon while .tarp.json quietly decided everything.
var ErrAmbiguousConfigFile = platformerrors.New("more than one tarp config file in the same directory")

// configFileExtensions are the formats a config file may be written in, in the
// order they are reported when a directory holds several.
var configFileExtensions = []string{".yaml", ".yml", ".json", ".toml"}

// Discover finds the config file governing dir: the nearest .tarp.{yaml,yml,
// json,toml} at or above it, stopping at the module root. It returns the empty
// string when there is none, which is not an error — running with no config
// file is the normal case and the defaults are a complete configuration.
//
// The walk goes upwards because a module's config belongs at its root and its
// packages are analyzed from wherever somebody happens to be standing:
// `tarp analyze` in internal/cli must mean the same thing it means from the
// root, or the exclusions a project agreed on hold only some of the time. It
// stops at the module root for the mirror-image reason — beyond it lies
// somebody's home directory, and a stray .tarp.yaml there should not silently
// govern every module underneath.
func Discover(dir string) (string, error) {
	current, err := filepath.Abs(dir)
	if err != nil {
		return "", platformerrors.Wrapf(err, "resolving %s", dir)
	}

	for {
		found, findErr := configFileIn(current)
		if findErr != nil || found != "" {
			return found, findErr
		}

		if isModuleRoot(current) {
			return "", nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", nil
		}

		current = parent
	}
}

// configFileIn returns the config file in exactly this directory, refusing to
// choose between two.
func configFileIn(dir string) (string, error) {
	found := make([]string, 0, len(configFileExtensions))

	for _, extension := range configFileExtensions {
		path := filepath.Join(dir, FileStem+extension)

		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			found = append(found, path)
		}
	}

	switch len(found) {
	case 0:
		return "", nil
	case 1:
		return found[0], nil
	default:
		return "", platformerrors.Wrapf(ErrAmbiguousConfigFile, "found %s", strings.Join(found, ", "))
	}
}

// isModuleRoot reports whether dir holds a go.mod.
func isModuleRoot(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, goModFile))

	return err == nil && !info.IsDir()
}

// LoadFromFile decodes a config file over the built-in defaults and then
// overlays environment variables (a set TARP_ variable wins over the file
// value). The format is chosen by the file's extension. The result is validated
// before it is returned.
//
// Decoding over the defaults rather than into a zero Config is what makes a
// three-line .tarp.yaml a sensible thing to write: a file says what it wants
// changed, and everything it leaves out keeps the value the binary would have
// used anyway. To turn something off, name it and give it the empty value — an
// explicit "provider": "" is still the opt-out into noop logging.
func LoadFromFile(ctx context.Context, path string) (*Config, error) {
	contentType, err := contentTypeOf(path)
	if err != nil {
		return nil, err
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, platformerrors.Wrapf(err, "reading configuration file %s", path)
	}

	// Decoded into an already-built Config rather than into a fresh one, which
	// is the whole of the overlay: every one of these three decoders leaves a
	// field alone when the document does not mention it, so the defaults survive
	// exactly the keys the file declines to have an opinion about. It is why
	// this reads the file itself instead of calling platform-go's
	// files.DecodeFile, which necessarily returns a zero value to decode into.
	cfg := New(Options{})
	if err = encoding.NewClientEncoder(contentType).Unmarshal(ctx, contents, cfg); err != nil {
		return nil, platformerrors.Wrapf(err, "decoding %s configuration file %s", contentType, path)
	}

	if err = applyEnvironmentVariables(cfg); err != nil {
		return nil, err
	}

	if err = cfg.Validate(ctx); err != nil {
		return nil, platformerrors.Wrapf(err, "validating configuration file %s", path)
	}

	return cfg, nil
}

// contentTypeOf maps a config file's extension onto the format to decode it as.
// The extension is the whole signal on purpose: sniffing the contents would
// mean a typo in a YAML file could be read as a TOML file that happens to parse.
func contentTypeOf(path string) (encoding.ContentType, error) {
	switch extension := strings.ToLower(filepath.Ext(path)); extension {
	case ".json":
		return encoding.ContentTypeJSON, nil
	case ".toml":
		return encoding.ContentTypeTOML, nil
	case ".yaml", ".yml":
		return encoding.ContentTypeYAML, nil
	default:
		return "", platformerrors.Wrapf(
			platformerrors.ErrUnrecognizedInputValue,
			"config file %s: expected one of %s", path, strings.Join(configFileExtensions, ", "),
		)
	}
}
