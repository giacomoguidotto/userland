package adapters

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/giacomoguidotto/userland/internal/platform"
)

func personalRepositories(c *Context, action Action) int {
	path := c.Env.Get("USERLAND_REPOSITORIES")
	if path == "" {
		path = filepath.Join(c.Env.Root, "cfg", "repositories.csv")
	}
	rows, err := readCSV(path, "github_repository", "home_relative_path")
	if err != nil {
		c.Log(Attention, err.Error())
		return 1
	}
	missing := false
	for _, row := range rows {
		target := filepath.Join(c.Env.Home, row[1])
		if exists(filepath.Join(target, ".git")) {
			continue
		}
		missing = true
		if exists(target) {
			c.Log(Attention, target+" exists and is not a Git checkout")
		} else {
			c.Log(Change, row[0]+" is missing at "+target)
		}
	}
	if action == Plan || action == Doctor {
		if !missing {
			level := Current
			if action == Doctor {
				level = Healthy
			}
			c.Log(level, "declared personal repositories exist")
			return 0
		}
		return 2
	}
	if !missing {
		return 0
	}
	gh, ok := commandPath(c, "", "gh")
	if !ok {
		c.Log(Attention, "GitHub CLI is unavailable")
		return 2
	}
	if run(c, gh, "auth", "status", "--hostname", "github.com").Code != 0 {
		c.Log(Manual, "authenticate GitHub with: gh auth login --hostname github.com --git-protocol https")
		return 2
	}
	for _, row := range rows {
		target := filepath.Join(c.Env.Home, row[1])
		if exists(filepath.Join(target, ".git")) {
			continue
		}
		if exists(target) {
			c.Log(Attention, "refused to replace "+target)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return 1
		}
		if result := run(c, gh, "repo", "clone", row[0], target, "--", "--filter=blob:none"); result.Code != 0 {
			return result.Code
		}
		c.Log(Changed, "cloned "+row[0])
	}
	return 0
}

func browserExtensions(c *Context, action Action) int {
	if !c.Env.IsMacOS() {
		return 0
	}
	rows, err := readCSV(filepath.Join(c.Env.Root, "cfg", "browser-extensions.csv"), "browser", "extension_id", "name")
	if err != nil {
		return 1
	}
	type missingExtension struct{ browser, id string }
	var missing []missingExtension
	for _, row := range rows {
		root := ""
		switch row[0] {
		case "helium":
			root = homePath(c, "Library", "Application Support", "net.imput.helium")
		case "chrome":
			root = homePath(c, "Library", "Application Support", "Google", "Chrome")
		default:
			return 1
		}
		installed := exists(filepath.Join(root, "Extensions", row[1]))
		profiles, _ := filepath.Glob(filepath.Join(root, "*", "Extensions", row[1]))
		installed = installed || len(profiles) != 0
		if !installed {
			c.Log(Manual, row[2]+" is missing from "+row[0])
			missing = append(missing, missingExtension{row[0], row[1]})
		}
	}
	if len(missing) == 0 {
		level := Current
		if action == Doctor {
			level = Healthy
		}
		c.Log(level, "declared browser extensions are installed")
		return 0
	}
	if action != Apply {
		return 2
	}
	c.Log(Manual, "opening the supported Chrome Web Store pages for missing extensions")
	for _, extension := range missing {
		application := "Helium"
		if extension.browser == "chrome" {
			application = "Google Chrome"
		}
		if result := run(c, "open", "-a", application, "https://chromewebstore.google.com/detail/"+extension.id); result.Code != 0 {
			return result.Code
		}
	}
	return 2
}

func fileHandlers(c *Context, action Action) int {
	if !c.Env.IsMacOS() {
		return 0
	}
	rows, err := readCSV(filepath.Join(c.Env.Root, "cfg", "file-handlers.csv"), "bundle_id", "extension_or_uti", "role")
	if err != nil {
		return 1
	}
	duti, ok := commandPath(c, "", "duti")
	if !ok {
		if action == Plan {
			c.Log(Change, "file-handler declarations will apply after duti is installed")
			return 0
		}
		message := "duti is unavailable"
		if action == Apply {
			message += "; skipped file-handler declarations"
		}
		c.Log(Attention, message)
		return 2
	}
	drift := false
	for _, row := range rows {
		result := run(c, duti, "-x", row[1])
		lines := strings.Split(string(result.Output), "\n")
		actual := ""
		if len(lines) >= 3 {
			actual = lines[2]
		}
		if actual == row[0] {
			continue
		}
		drift = true
		if action == Apply {
			if result := run(c, duti, "-s", row[0], row[1], row[2]); result.Code != 0 {
				return result.Code
			}
			c.Log(Changed, "assigned ."+row[1]+" to "+row[0])
		} else {
			c.Log(Attention, "."+row[1]+" is not assigned to "+row[0])
		}
	}
	if drift && action != Apply {
		return 2
	}
	if !drift {
		level := Current
		if action == Doctor {
			level = Healthy
		}
		c.Log(level, "declared file handlers match")
	}
	return 0
}

func manualApps(c *Context, action Action) int {
	if !c.Env.IsMacOS() || action == Apply {
		return 0
	}
	rows, err := readCSV(filepath.Join(c.Env.Root, "cfg", "manual-apps.csv"), "name", "application_path", "reason")
	if err != nil {
		return 1
	}
	missing := false
	for _, row := range rows {
		if exists(row[1]) {
			c.Log(Healthy, row[0]+" is installed")
		} else {
			missing = true
			c.Log(Manual, row[0]+": "+row[2])
		}
	}
	if missing {
		return 2
	}
	return 0
}

func repositorySnapshot(c *Context, action Action) int {
	if action == Apply {
		return 0
	}
	fresh := repositorySnapshotFresh(c.Env)
	if action == Plan {
		if fresh {
			c.Log(Current, "repository snapshot is current")
		} else {
			c.Log(Change, "repository snapshot will refresh during plan")
		}
		return 0
	}
	if fresh {
		c.Log(Healthy, "repository snapshot is current")
		return 0
	}
	c.Log(Attention, "repository snapshot is missing or older than 24 hours")
	return 2
}

func repositorySnapshotFresh(env platform.Environment) bool {
	snapshot := filepath.Join(env.Cache, "repositories.csv")
	meta := filepath.Join(env.Cache, "repositories.meta")
	roots := env.Get("USERLAND_REPO_ROOTS")
	if roots == "" {
		roots = filepath.Join(env.Home, "dev", "life") + ":" + filepath.Join(env.Home, "dev", "uni")
	}
	contents, err := os.ReadFile(meta)
	if err != nil || firstLine(contents) != "v2 "+roots {
		return false
	}
	info, err := os.Stat(snapshot)
	return err == nil && time.Since(info.ModTime()) < time.Duration(env.RepositoryTTL())*time.Second
}

func raycast(c *Context, action Action) int {
	if !c.Env.IsMacOS() {
		return 0
	}
	export := c.Env.Get("USERLAND_RAYCAST_EXPORT")
	if export == "" {
		export = filepath.Join(c.Env.Root, "cfg", "raycast.rayconfig")
	}
	receipt := filepath.Join(c.Env.State, "receipts", "raycast-import.sha256")
	eligible := raycastEligible(export)
	current := false
	if eligible {
		digest, _ := fileSHA256(export)
		stored, _ := os.ReadFile(receipt)
		current = digest == firstLine(stored)
	}
	if action == Plan {
		if current {
			c.Log(Current, "Raycast configuration import has a matching receipt")
		} else if eligible {
			c.Log(Manual, "Raycast will open its encrypted configuration import")
		} else {
			c.Log(Manual, "Raycast has no encrypted configuration export")
		}
		return 0
	}
	if action == Doctor {
		if !eligible {
			c.Log(Attention, "Raycast configuration is missing or is not encrypted")
			return 2
		}
		if !current {
			c.Log(Attention, "Raycast configuration needs an attended import")
			return 2
		}
		c.Log(Healthy, "Raycast import acknowledgement matches the encrypted export")
		return 0
	}
	if current {
		return 0
	}
	if !eligible {
		c.Log(Manual, "export an encrypted Raycast configuration before importing it")
		return 2
	}
	if !exists("/Applications/Raycast.app") && !exists(homePath(c, "Applications", "Raycast.app")) {
		c.Log(Manual, "install Raycast, then rerun sync to import its configuration")
		return 2
	}
	if !c.Terminal {
		c.Log(Manual, "Raycast import requires an interactive terminal")
		return 2
	}
	c.Log(Manual, "opening Raycast configuration import; enter its export passphrase in Raycast")
	if result := run(c, "open", "-a", "Raycast", export); result.Code != 0 {
		return result.Code
	}
	// The caller owns the interactive acknowledgement and only invokes this
	// adapter with a confirmed input stream.
	if _, err := bufio.NewReader(c.Stdin).ReadString('\n'); err != nil && err != io.EOF {
		c.Log(Manual, "Raycast import was not confirmed; no receipt was recorded")
		return 2
	}
	if err := os.MkdirAll(filepath.Dir(receipt), 0o700); err != nil {
		return 1
	}
	digest, _ := fileSHA256(export)
	if err := os.WriteFile(receipt, []byte(digest+"\n"), 0o600); err != nil {
		return 1
	}
	c.Log(Changed, "recorded the confirmed Raycast import")
	return 0
}

func raycastEligible(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 1024 || filepath.Ext(path) != ".rayconfig" {
		return false
	}
	contents, err := os.ReadFile(path)
	if err != nil || len(contents) < 12 {
		return false
	}
	if bytes.HasPrefix(contents, []byte{0x1f, 0x8b}) {
		return gzipContains(contents, `"data":"`, `"encryption":{`, `"salt":"`, `"iv":"`, `"authTag":"`)
	}
	if bytes.HasPrefix(contents, []byte("RAYCFG3\n")) {
		length := int(binary.LittleEndian.Uint32(contents[8:12]))
		return length > 0 && 12+length < len(contents) && gzipContains(contents[12:12+length], `"schemaVersion":3`, `"encryption":{`, `"salt":"`, `"iv":"`)
	}
	return true
}

func gzipContains(encoded []byte, markers ...string) bool {
	reader, err := gzip.NewReader(bytes.NewReader(encoded))
	if err != nil {
		return false
	}
	decoded, err := io.ReadAll(reader)
	if closeErr := reader.Close(); err != nil || closeErr != nil {
		return false
	}
	for _, marker := range markers {
		if !bytes.Contains(decoded, []byte(marker)) {
			return false
		}
	}
	return true
}

func securityHealth(c *Context, action Action) int {
	if !c.Env.IsMacOS() || action != Doctor {
		return 0
	}
	attention := false
	checks := []struct {
		command string
		args    []string
		match   string
		good    string
		bad     string
	}{
		{"fdesetup", []string{"status"}, "FileVault is On", "FileVault is on", "FileVault is not on"},
		{"csrutil", []string{"status"}, "enabled", "System Integrity Protection is enabled", "System Integrity Protection is not enabled"},
		{"softwareupdate", []string{"--schedule"}, "on", "automatic update checks are on", "automatic update checks are off"},
	}
	for _, check := range checks {
		result := run(c, check.command, check.args...)
		if result.Code == 0 && strings.Contains(strings.ToLower(string(result.Output)), strings.ToLower(check.match)) {
			c.Log(Healthy, check.good)
		} else {
			attention = true
			c.Log(Attention, check.bad)
		}
	}
	if run(c, "tmutil", "destinationinfo").Code == 0 {
		c.Log(Healthy, "a Time Machine destination is configured")
	} else {
		attention = true
		c.Log(Attention, "no Time Machine destination is configured")
	}
	result := run(c, "df", "-Pk", "/")
	fields := strings.Fields(string(result.Output))
	free := int64(0)
	if len(fields) >= 11 {
		free, _ = strconv.ParseInt(fields[len(fields)-3], 10, 64)
	}
	if free >= 31457280 {
		c.Log(Healthy, "at least 30 GiB is free for large developer applications")
	} else {
		attention = true
		c.Log(Attention, "less than 30 GiB is free; Xcode or simulator installs may fail")
	}
	if attention {
		return 2
	}
	return 0
}
