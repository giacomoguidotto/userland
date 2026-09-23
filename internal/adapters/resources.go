package adapters

import (
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
	repositorycatalog "github.com/giacomoguidotto/userland/internal/repository"
	"github.com/giacomoguidotto/userland/internal/tui"
)

func personalRepositories(c *Context, action Action) int {
	path := c.Env.Get("USERLAND_REPOSITORIES")
	if path == "" {
		path = filepath.Join(c.Env.Root, "cfg", "repositories.csv")
	}
	rows, err := readCSV(path, "repository", "home_relative_path", "branch")
	if err != nil {
		c.Log(Attention, err.Error())
		return 1
	}
	drift := false
	for _, row := range rows {
		target := filepath.Join(c.Env.Home, row[1])
		result := repositorycatalog.InspectCanonical(c.Context, c.Env, target, row[0], row[2])
		if result.Status == repositorycatalog.CanonicalCurrent {
			continue
		}
		drift = true
		level := Change
		if result.Status == repositorycatalog.CanonicalAttention {
			level = Attention
		}
		c.Log(level, row[1]+" "+result.Message)
	}
	if action == Plan || action == Doctor {
		if !drift {
			level := Current
			if action == Doctor {
				level = Healthy
			}
			c.Log(level, "declared personal repositories exist")
			return 0
		}
		return 2
	}
	if !drift {
		return 0
	}
	for _, row := range rows {
		target := filepath.Join(c.Env.Home, row[1])
		inspected := repositorycatalog.InspectCanonical(c.Context, c.Env, target, row[0], row[2])
		if inspected.Status == repositorycatalog.CanonicalCurrent {
			continue
		}
		result := repositorycatalog.ReconcileCanonical(c.Context, c.Env, target, row[0], row[2])
		if result.Status == repositorycatalog.CanonicalAttention {
			c.Log(Attention, row[1]+" "+result.Message)
			return 2
		}
		c.Log(Changed, row[1]+" "+result.Message)
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
	type missingExtension struct{ browser, id, name, root string }
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
		if !browserExtensionInstalled(root, row[1]) {
			if action != Apply {
				c.Log(Manual, row[2]+" is missing from "+row[0])
			}
			missing = append(missing, missingExtension{row[0], row[1], row[2], root})
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
	if !c.Terminal {
		c.Log(Manual, "Browser extensions require installation confirmation in the browser; rerun sync in a terminal")
		return 2
	}
	wizard := tui.Wizard{Render: tui.New(c.Output, c.Env.List), Input: c.Stdin}
	wizard.Render.Section("Browser extensions")
	incomplete := false
	for _, extension := range missing {
		application := "Helium"
		if extension.browser == "chrome" {
			application = "Google Chrome"
		}
		wizard.Info("Install " + extension.name + " in " + application + ". On the store page, click Add to Chrome and confirm Add extension in the browser. Opening the page alone does not install it.")
		if result := run(c, "open", "-a", application, "https://chromewebstore.google.com/detail/"+extension.id); result.Code != 0 {
			c.Log(Attention, "Could not open the extension store page: "+strings.TrimSpace(string(result.Output)))
			return result.Code
		}
		if code := wizard.Continue("Check installation after the browser finishes"); code != 0 {
			return code
		}
		if browserExtensionInstalled(extension.root, extension.id) {
			c.Log(Changed, extension.name+" installation detected in "+application)
		} else {
			incomplete = true
			c.Log(Manual, extension.name+" is still missing from "+application+"; complete the browser installation and rerun sync")
		}
	}
	if incomplete {
		return 2
	}
	return 0
}

func browserExtensionInstalled(root, id string) bool {
	profiles, _ := filepath.Glob(filepath.Join(root, "*", "Extensions", id))
	return exists(filepath.Join(root, "Extensions", id)) || len(profiles) != 0
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
		roots = filepath.Join(env.Home, "dev", "life") + ":" + filepath.Join(env.Home, "dev", "research")
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
		c.Log(Current, "Raycast configuration is imported; its declared login item starts it automatically")
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
	wizard := tui.Wizard{Render: tui.New(c.Output, c.Env.List), Input: c.Stdin}
	wizard.Render.Section("Raycast configuration")
	wizard.Info("Finish or dismiss Raycast's onboarding first. Return here when its command search is available; Userland will then open the export.")
	if result := run(c, "open", "-a", "Raycast"); result.Code != 0 {
		c.Log(Attention, "Could not launch Raycast: "+strings.TrimSpace(string(result.Output)))
		return result.Code
	}
	ready, code := wizard.ConfirmDone("Is Raycast ready for import?")
	if code != 0 {
		return code
	}
	if !ready {
		c.Log(Manual, "Raycast onboarding is unfinished; rerun sync when ready. No import receipt was recorded")
		return 2
	}
	wizard.Info("Enter the export passphrase in Raycast and finish the import. Raycast is declared to start automatically at login.")
	wizard.Info("If no import dialog appears, run Import Settings & Data in Raycast and choose this file: " + export)
	if result := run(c, "open", "-a", "Raycast", export); result.Code != 0 {
		c.Log(Attention, "Could not open the Raycast export: "+strings.TrimSpace(string(result.Output)))
		return result.Code
	}
	confirmed, code := wizard.ConfirmDone("Confirm Raycast import completed")
	if code != 0 {
		return code
	}
	if !confirmed {
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
