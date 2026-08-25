package adapters

import (
	"os"
	"path/filepath"
	"strings"
)

var androidPackages = []string{"platform-tools", "emulator", "platforms;android-36", "build-tools;36.0.0"}

func androidSDK(c *Context, action Action) int {
	if !c.Env.IsMacOS() {
		return 0
	}
	home := c.Env.Get("ANDROID_HOME")
	if home == "" {
		home = homePath(c, "Library", "Android", "sdk")
	}
	sdkmanager, sdkOK := commandPath(c, "USERLAND_SDKMANAGER", "sdkmanager")
	java := ""
	if c.Env.Mise != "" && executable(c.Env.Mise) {
		result := runMise(c, "where", "java")
		if result.Code == 0 {
			java = firstLine(result.Output)
		}
	}
	runtimeOK := sdkOK && executable(filepath.Join(java, "bin", "java"))
	if runtimeOK {
		environ := c.Env.With("JAVA_HOME", java, "PATH", filepath.Join(java, "bin")+":"+c.Env.Get("PATH"))
		runtimeOK = runWith(c, environ, nil, sdkmanager, "--version").Code == 0
	}
	complete := androidComplete(home)
	if action == Plan {
		if !runtimeOK {
			c.Log(Change, "Android sdkmanager will use pinned Java 21 and JAVA_HOME")
		}
		if complete {
			c.Log(Current, "Android SDK, emulator, platform tools, and API 36 are installed")
		} else {
			c.Log(Manual, "Android SDK packages and licenses need installation")
		}
		return 0
	}
	if !runtimeOK {
		c.Log(Attention, "sdkmanager cannot execute with pinned Java 21 and JAVA_HOME")
		return 2
	}
	if action == Doctor {
		if complete {
			c.Log(Healthy, "Android command-line development environment is complete")
			return 0
		}
		c.Log(Attention, "Android command-line development packages are missing")
		return 2
	}
	if complete {
		return 0
	}
	if !c.Terminal {
		c.Log(Manual, "Android licenses require an interactive terminal")
		return 2
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return 1
	}
	c.Log(Manual, "review and accept the Android SDK licenses")
	environ := c.Env.With(
		"JAVA_HOME", java,
		"ANDROID_HOME", home,
		"PATH", filepath.Join(java, "bin")+":"+c.Env.Get("PATH"),
	)
	if result := runWith(c, environ, c.Stdin, sdkmanager, "--licenses"); result.Code != 0 {
		return result.Code
	}
	if result := runWith(c, environ, c.Stdin, sdkmanager, androidPackages...); result.Code != 0 {
		return result.Code
	}
	c.Log(Changed, "installed Android command-line development packages")
	return 0
}

func androidComplete(home string) bool {
	checks := []string{
		filepath.Join(home, "platform-tools", "adb"),
		filepath.Join(home, "emulator", "emulator"),
		filepath.Join(home, "platforms", "android-36"),
		filepath.Join(home, "build-tools", "36.0.0"),
	}
	for index, path := range checks {
		info, err := os.Stat(path)
		if err != nil {
			return false
		}
		if index < 2 && info.Mode()&0o111 == 0 || index >= 2 && !info.IsDir() {
			return false
		}
	}
	return true
}

func splitWords(value string) []string { return strings.Fields(value) }
