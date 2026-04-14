package devcleanup

import (
	"path/filepath"
)

type staticProvider struct {
	id    string
	tasks []CleanupTask
}

func (p staticProvider) ID() string { return p.id }

func (p staticProvider) Tasks(_ Environment) []CleanupTask {
	out := make([]CleanupTask, len(p.tasks))
	copy(out, p.tasks)
	return out
}

func BuiltinProviders(env Environment) []Provider {
	home := env.HomeDir
	temp := env.TempDir

	tasks := []CleanupTask{
		// .NET / NuGet
		pathTask("nuget-packages", "NuGet package cache", "package-manager", RiskSafe, filepath.Join(home, ".nuget", "packages")),
		pathTask("dotnet-http-cache", ".NET HTTP cache", "package-manager", RiskSafe, filepath.Join(home, ".local", "share", "NuGet", "v3-cache")),
		pathTask("dotnet-tool-cache", ".NET tools cache", "package-manager", RiskSafe, filepath.Join(home, ".dotnet", "tools")),
		pathTask("dotnet-cli-cache", ".NET CLI cache", "package-manager", RiskSafe, filepath.Join(home, ".dotnet")),

		// Node / JS
		pathTask("npm-cache", "npm cache", "package-manager", RiskSafe, filepath.Join(home, ".npm")),
		pathTask("npm-logs", "npm log files", "logs", RiskSafe, filepath.Join(home, ".npm", "_logs")),
		pathTask("yarn-cache", "Yarn cache", "package-manager", RiskSafe, filepath.Join(home, ".cache", "yarn")),
		pathTask("pnpm-store", "pnpm store", "package-manager", RiskSafe, filepath.Join(home, ".pnpm-store")),
		pathTask("pnpm-cache", "pnpm cache", "package-manager", RiskSafe, filepath.Join(home, ".cache", "pnpm")),
		pathTask("parcel-cache", "Parcel bundler cache", "build-artifact", RiskSafe, filepath.Join(home, ".parcel-cache")),
		pathTask("vite-cache", "Vite cache", "build-artifact", RiskSafe, filepath.Join(home, ".vite")),

		// Python
		pathTask("pip-cache", "pip cache", "package-manager", RiskSafe, filepath.Join(home, ".cache", "pip")),
		pathTask("pipenv-cache", "Pipenv cache", "package-manager", RiskSafe, filepath.Join(home, ".cache", "pipenv")),
		pathTask("poetry-cache", "Poetry cache", "package-manager", RiskSafe, filepath.Join(home, ".cache", "pypoetry")),

		// Rust
		pathTask("cargo-registry", "Cargo registry cache", "package-manager", RiskSafe, filepath.Join(home, ".cargo", "registry")),
		pathTask("cargo-git", "Cargo git cache", "package-manager", RiskSafe, filepath.Join(home, ".cargo", "git")),

		// Java / Kotlin / Android
		pathTask("gradle-cache", "Gradle cache", "package-manager", RiskSafe, filepath.Join(home, ".gradle", "caches")),
		pathTask("gradle-daemon", "Gradle daemon data", "package-manager", RiskModerate, filepath.Join(home, ".gradle", "daemon")),
		pathTask("gradle-kotlin-dsl", "Gradle Kotlin DSL cache", "package-manager", RiskSafe, filepath.Join(home, ".gradle", "kotlin")),
		pathTask("maven-cache", "Maven local repo", "package-manager", RiskSafe, filepath.Join(home, ".m2", "repository")),
		pathTask("android-studio-system", "Android Studio system cache", "ide", RiskModerate, filepath.Join(home, ".AndroidStudio", "system")),
		pathTask("android-avd-temp", "Android emulator temp data", "android", RiskModerate, filepath.Join(home, ".android", "avd")),
		pathTask("android-sdk-cache", "Android SDK temp/cache", "android", RiskModerate, filepath.Join(home, "Android", "Sdk", ".temp")),
		pathTask("android-gradle-cache", "Android Gradle build cache", "android", RiskSafe, filepath.Join(home, ".android", "build-cache")),

		// Flutter / Dart
		pathTask("flutter-pub-cache", "Flutter pub cache", "flutter", RiskSafe, filepath.Join(home, ".pub-cache")),
		pathTask("flutter-tool-cache", "Flutter tool cache", "flutter", RiskSafe, filepath.Join(home, "flutter", "bin", "cache")),
		pathTask("dart-tool-cache", "Dart tool cache", "flutter", RiskSafe, filepath.Join(home, ".dartServer")),

		// Go
		pathTask("go-build-cache", "Go build cache", "package-manager", RiskSafe, filepath.Join(home, "AppData", "Local", "go-build")),
		pathTask("go-mod-cache", "Go module cache", "package-manager", RiskSafe, filepath.Join(home, "go", "pkg", "mod")),

		// Generic temp/cache/logs
		pathTask("dev-temp", "User temporary files", "os-temp", RiskSafe, temp),
		pathTask("general-cache", "General cache folder", "os-temp", RiskModerate, filepath.Join(home, ".cache")),
		pathTask("tmp-folder", "Temporary folder", "os-temp", RiskSafe, filepath.Join(string(filepath.Separator), "tmp")),
		pathTask("tmp-var-folder", "Var temp folder", "os-temp", RiskModerate, filepath.Join(string(filepath.Separator), "var", "tmp")),
		pathTask("vscode-cache", "VS Code cache", "ide", RiskModerate, filepath.Join(home, ".config", "Code", "Cache")),
		pathTask("vscode-workspace-storage", "VS Code workspace storage", "ide", RiskModerate, filepath.Join(home, ".config", "Code", "User", "workspaceStorage")),
		pathTask("jetbrains-cache", "JetBrains caches", "ide", RiskModerate, filepath.Join(home, ".cache", "JetBrains")),
		pathTask("docker-desktop-cache", "Docker desktop cache", "container", RiskModerate, filepath.Join(home, ".docker", "buildx")),
		pathTask("browser-cache-chrome", "Chrome cache", "browser", RiskModerate, filepath.Join(home, ".cache", "google-chrome")),
		pathTask("browser-cache-firefox", "Firefox cache", "browser", RiskModerate, filepath.Join(home, ".cache", "mozilla", "firefox")),
		pathTask("browser-cache-brave", "Brave cache", "browser", RiskModerate, filepath.Join(home, ".cache", "BraveSoftware", "Brave-Browser")),
		pathTask("browser-cache-opera", "Opera cache", "browser", RiskModerate, filepath.Join(home, ".cache", "opera")),
		pathTask("browser-cache-chromium", "Chromium cache", "browser", RiskModerate, filepath.Join(home, ".cache", "chromium")),
		pathTask("crash-dumps", "Crash dumps", "logs", RiskModerate, filepath.Join(home, ".local", "share", "CrashDumps")),
		pathTask("app-logs", "Application logs", "logs", RiskModerate, filepath.Join(home, ".local", "share", "logs")),
		pathTask("react-native-metro-cache", "React Native Metro cache", "build-artifact", RiskSafe, filepath.Join(home, ".metro-cache")),
		pathTask("nextjs-cache", "Next.js cache", "build-artifact", RiskSafe, filepath.Join(home, ".next", "cache")),
		pathTask("nuxt-cache", "Nuxt cache", "build-artifact", RiskSafe, filepath.Join(home, ".nuxt")),
		pathTask("svelte-cache", "Svelte cache", "build-artifact", RiskSafe, filepath.Join(home, ".svelte-kit")),
		pathTask("angular-cache", "Angular cache", "build-artifact", RiskSafe, filepath.Join(home, ".angular", "cache")),
		// Gamer-focused safe/moderate cleanup (cache/log/temp only, no save data).
		pathTask("steam-shader-cache", "Steam shader cache", "gaming", RiskSafe, filepath.Join(home, ".steam", "steam", "steamapps", "shadercache")),
		pathTask("steam-download-cache", "Steam download cache", "gaming", RiskModerate, filepath.Join(home, ".steam", "steam", "steamapps", "downloading")),
		pathTask("epic-launcher-cache", "Epic Games launcher cache", "gaming", RiskSafe, filepath.Join(home, ".config", "Epic", "EpicGamesLauncher", "Saved", "webcache")),
		pathTask("battle-net-cache", "Battle.net cache", "gaming", RiskSafe, filepath.Join(home, ".cache", "Blizzard", "Battle.net")),
		pathTask("discord-cache", "Discord cache", "gaming", RiskSafe, filepath.Join(home, ".config", "discord", "Cache")),
		pathTask("nvidia-dx-cache", "NVIDIA DirectX cache", "gaming", RiskSafe, filepath.Join(home, ".nv", "GLCache")),
		patternTask("project-build-artifacts", "Project build artifacts", "build-artifact", RiskAggressive, []string{"bin", "obj", "dist", "target"}),
		commandTask("dotnet-locals", ".NET CLI cache cleanup", "package-manager", RiskSafe, "dotnet", "nuget", "locals", "all", "--clear"),
		commandTask("npm-clean-force", "npm force clean", "package-manager", RiskModerate, "npm", "cache", "clean", "--force"),
		commandTask("docker-prune", "Docker prune (images/volumes)", "container", RiskAggressive, "docker", "system", "prune", "-a", "--volumes", "-f"),
	}

	if env.OS == "windows" {
		tasks = append(tasks,
			pathTask("vscode-cache-win", "VS Code cache (Windows)", "ide", RiskModerate, filepath.Join(home, "AppData", "Roaming", "Code", "Cache")),
			pathTask("vscode-workspace-win", "VS Code workspace storage (Windows)", "ide", RiskModerate, filepath.Join(home, "AppData", "Roaming", "Code", "User", "workspaceStorage")),
			pathTask("visual-studio-cache-win", "Visual Studio component cache", "ide", RiskModerate, filepath.Join(home, "AppData", "Local", "Microsoft", "VisualStudio")),
			pathTask("windows-temp", "Windows temp", "os-temp", RiskSafe, filepath.Join(home, "AppData", "Local", "Temp")),
			pathTask("windows-temp-system", "Windows system temp", "os-temp", RiskSafe, filepath.Join(home, "AppData", "Local", "Microsoft", "Windows", "INetCache")),
			pathTask("windows-shader-cache", "Windows shader cache", "os-temp", RiskSafe, filepath.Join(home, "AppData", "Local", "D3DSCache")),
			pathTask("windows-crash-dumps", "Windows app crash dumps", "logs", RiskModerate, filepath.Join(home, "AppData", "Local", "CrashDumps")),
			pathTask("windows-prefetch", "Windows prefetch cache", "os-temp", RiskModerate, filepath.Join("C:", "Windows", "Prefetch")),
			pathTask("browser-cache-edge-win", "Edge cache", "browser", RiskModerate, filepath.Join(home, "AppData", "Local", "Microsoft", "Edge", "User Data", "Default", "Cache")),
			pathTask("browser-cache-chrome-win", "Chrome cache (Windows)", "browser", RiskModerate, filepath.Join(home, "AppData", "Local", "Google", "Chrome", "User Data", "Default", "Cache")),
			pathTask("browser-cache-firefox-win", "Firefox cache (Windows)", "browser", RiskModerate, filepath.Join(home, "AppData", "Local", "Mozilla", "Firefox", "Profiles")),
			pathTask("browser-cache-brave-win", "Brave cache (Windows)", "browser", RiskModerate, filepath.Join(home, "AppData", "Local", "BraveSoftware", "Brave-Browser", "User Data", "Default", "Cache")),
			pathTask("browser-cache-opera-win", "Opera cache (Windows)", "browser", RiskModerate, filepath.Join(home, "AppData", "Roaming", "Opera Software", "Opera Stable", "Cache")),
			pathTask("browser-cache-opera-gx-win", "Opera GX cache (Windows)", "browser", RiskModerate, filepath.Join(home, "AppData", "Roaming", "Opera Software", "Opera GX Stable", "Cache")),
			pathTask("browser-cache-chromium-win", "Chromium cache (Windows)", "browser", RiskModerate, filepath.Join(home, "AppData", "Local", "Chromium", "User Data", "Default", "Cache")),
			pathTask("windows-android-studio", "Android Studio cache (Windows)", "android", RiskModerate, filepath.Join(home, "AppData", "Local", "Google", "AndroidStudio")),
			pathTask("windows-flutter-pub-cache", "Flutter pub cache (Windows)", "flutter", RiskSafe, filepath.Join(home, "AppData", "Local", "Pub", "Cache")),
			pathTask("windows-ios-sim-cache", "iOS simulator cache (Windows tooling)", "ios", RiskAggressive, filepath.Join(home, "AppData", "Local", "Xamarin", "iOS")),
			pathTask("steam-html-cache-win", "Steam web cache (Windows)", "gaming", RiskSafe, filepath.Join(home, "AppData", "Local", "Steam", "htmlcache")),
			pathTask("steam-shader-cache-win", "Steam shader cache (Windows)", "gaming", RiskSafe, filepath.Join(home, "AppData", "Local", "Steam", "steamapps", "shadercache")),
			pathTask("epic-launcher-cache-win", "Epic Games launcher cache (Windows)", "gaming", RiskSafe, filepath.Join(home, "AppData", "Local", "EpicGamesLauncher", "Saved", "webcache")),
			pathTask("ea-app-cache-win", "EA app cache (Windows)", "gaming", RiskModerate, filepath.Join(home, "AppData", "Local", "Electronic Arts", "EA Desktop", "Cache")),
			pathTask("riot-client-cache-win", "Riot client cache (Windows)", "gaming", RiskModerate, filepath.Join(home, "AppData", "Local", "Riot Games", "Riot Client", "Cache")),
			pathTask("battle-net-cache-win", "Battle.net cache (Windows)", "gaming", RiskSafe, filepath.Join(home, "AppData", "ProgramData", "Battle.net", "Cache")),
			pathTask("ubisoft-connect-cache-win", "Ubisoft Connect cache (Windows)", "gaming", RiskModerate, filepath.Join(home, "AppData", "Local", "Ubisoft Game Launcher", "cache")),
			pathTask("discord-cache-win", "Discord cache (Windows)", "gaming", RiskSafe, filepath.Join(home, "AppData", "Roaming", "discord", "Cache")),
			pathTask("nvidia-shader-cache-win", "NVIDIA shader cache (Windows)", "gaming", RiskSafe, filepath.Join(home, "AppData", "Local", "NVIDIA", "DXCache")),
			pathTask("amd-shader-cache-win", "AMD shader cache (Windows)", "gaming", RiskSafe, filepath.Join(home, "AppData", "Local", "AMD", "DxCache")),
		)
	}

	if env.OS == "darwin" {
		tasks = append(tasks,
			pathTask("xcode-derived-data", "Xcode derived data", "build-artifact", RiskModerate, filepath.Join(home, "Library", "Developer", "Xcode", "DerivedData")),
			pathTask("xcode-archives", "Xcode archives", "build-artifact", RiskModerate, filepath.Join(home, "Library", "Developer", "Xcode", "Archives")),
			pathTask("ios-sim-caches", "iOS Simulator caches", "ios", RiskModerate, filepath.Join(home, "Library", "Developer", "CoreSimulator", "Caches")),
			pathTask("ios-sim-devices", "iOS Simulator devices data", "ios", RiskAggressive, filepath.Join(home, "Library", "Developer", "CoreSimulator", "Devices")),
			pathTask("vscode-cache-macos", "VS Code cache (macOS)", "ide", RiskModerate, filepath.Join(home, "Library", "Application Support", "Code", "Cache")),
			pathTask("jetbrains-cache-macos", "JetBrains caches (macOS)", "ide", RiskModerate, filepath.Join(home, "Library", "Caches", "JetBrains")),
			pathTask("macos-logs", "macOS application logs", "logs", RiskModerate, filepath.Join(home, "Library", "Logs")),
			pathTask("macos-temp", "macOS temporary files", "os-temp", RiskSafe, filepath.Join(home, "Library", "Caches")),
			pathTask("safari-cache-macos", "Safari cache", "browser", RiskModerate, filepath.Join(home, "Library", "Caches", "com.apple.Safari")),
			pathTask("chrome-cache-macos", "Chrome cache (macOS)", "browser", RiskModerate, filepath.Join(home, "Library", "Caches", "Google", "Chrome")),
			pathTask("firefox-cache-macos", "Firefox cache (macOS)", "browser", RiskModerate, filepath.Join(home, "Library", "Caches", "Firefox")),
			pathTask("brave-cache-macos", "Brave cache (macOS)", "browser", RiskModerate, filepath.Join(home, "Library", "Caches", "BraveSoftware", "Brave-Browser")),
			pathTask("edge-cache-macos", "Edge cache (macOS)", "browser", RiskModerate, filepath.Join(home, "Library", "Caches", "Microsoft Edge")),
			pathTask("opera-cache-macos", "Opera cache (macOS)", "browser", RiskModerate, filepath.Join(home, "Library", "Caches", "com.operasoftware.Opera")),
			pathTask("cocoapods-cache-macos", "CocoaPods cache", "ios", RiskSafe, filepath.Join(home, "Library", "Caches", "CocoaPods")),
			pathTask("swiftpm-cache-macos", "Swift package cache", "ios", RiskSafe, filepath.Join(home, "Library", "Caches", "org.swift.swiftpm")),
			pathTask("flutter-macos-cache", "Flutter cache (macOS)", "flutter", RiskSafe, filepath.Join(home, ".pub-cache")),
			pathTask("steam-cache-macos", "Steam cache (macOS)", "gaming", RiskSafe, filepath.Join(home, "Library", "Application Support", "Steam", "appcache")),
			pathTask("steam-shader-cache-macos", "Steam shader cache (macOS)", "gaming", RiskSafe, filepath.Join(home, "Library", "Application Support", "Steam", "steamapps", "shadercache")),
			pathTask("epic-cache-macos", "Epic Games cache (macOS)", "gaming", RiskSafe, filepath.Join(home, "Library", "Caches", "com.epicgames.EpicGamesLauncher")),
			pathTask("discord-cache-macos", "Discord cache (macOS)", "gaming", RiskSafe, filepath.Join(home, "Library", "Application Support", "discord", "Cache")),
		)
	}

	if env.OS == "linux" {
		tasks = append(tasks,
			pathTask("thumbnails-linux", "Desktop thumbnails", "os-temp", RiskSafe, filepath.Join(home, ".cache", "thumbnails")),
			pathTask("trash-files-linux", "Trash files", "os-temp", RiskModerate, filepath.Join(home, ".local", "share", "Trash", "files")),
			pathTask("journal-cache-linux", "Journal/log cache", "logs", RiskModerate, filepath.Join(home, ".cache", "journal")),
			pathTask("apt-cache-linux", "APT package cache", "package-manager", RiskModerate, filepath.Join(string(filepath.Separator), "var", "cache", "apt", "archives")),
			pathTask("npm-cache-linux", "npm cache (Linux)", "package-manager", RiskSafe, filepath.Join(home, ".cache", "npm")),
			pathTask("gradle-daemon-linux", "Gradle daemon cache (Linux)", "package-manager", RiskModerate, filepath.Join(home, ".gradle", "daemon")),
			pathTask("android-sdk-linux-cache", "Android SDK cache (Linux)", "android", RiskModerate, filepath.Join(home, "Android", "Sdk", ".temp")),
			pathTask("flutter-linux-cache", "Flutter cache (Linux)", "flutter", RiskSafe, filepath.Join(home, ".pub-cache")),
			pathTask("steam-cache-linux", "Steam cache (Linux)", "gaming", RiskSafe, filepath.Join(home, ".steam", "steam", "appcache")),
			pathTask("steam-shader-cache-linux", "Steam shader cache (Linux)", "gaming", RiskSafe, filepath.Join(home, ".steam", "steam", "steamapps", "shadercache")),
			pathTask("lutris-cache-linux", "Lutris cache (Linux)", "gaming", RiskModerate, filepath.Join(home, ".cache", "lutris")),
			pathTask("heroic-cache-linux", "Heroic Games launcher cache", "gaming", RiskSafe, filepath.Join(home, ".config", "heroic")),
			pathTask("discord-cache-linux", "Discord cache (Linux)", "gaming", RiskSafe, filepath.Join(home, ".config", "discord", "Cache")),
			pathTask("edge-cache-linux", "Edge cache (Linux)", "browser", RiskModerate, filepath.Join(home, ".cache", "microsoft-edge")),
			pathTask("brave-cache-linux", "Brave cache (Linux)", "browser", RiskModerate, filepath.Join(home, ".cache", "BraveSoftware", "Brave-Browser")),
			pathTask("opera-cache-linux", "Opera cache (Linux)", "browser", RiskModerate, filepath.Join(home, ".cache", "opera")),
			pathTask("chromium-cache-linux", "Chromium cache (Linux)", "browser", RiskModerate, filepath.Join(home, ".cache", "chromium")),
		)
	}

	return []Provider{staticProvider{id: "builtin", tasks: tasks}}
}

func pathTask(id, name, category string, risk RiskLevel, path string) CleanupTask {
	hints := []string{}
	switch category {
	case "ide":
		hints = []string{"code", "devenv", "idea64", "rider64", "studio64"}
	case "browser":
		hints = []string{"chrome", "msedge", "firefox", "brave"}
	}
	return CleanupTask{
		ID:           id,
		Kind:         TaskKindPath,
		Name:         name,
		Category:     category,
		Description:  "cleanup path contents",
		Risk:         risk,
		ProcessHints: hints,
		PathTask: &PathTask{
			Path:            path,
			RemoveDirectory: false,
		},
	}
}

func commandTask(id, name, category string, risk RiskLevel, executable string, args ...string) CleanupTask {
	return CleanupTask{
		ID:          id,
		Kind:        TaskKindCommand,
		Name:        name,
		Category:    category,
		Description: "execute cleanup command",
		Risk:        risk,
		CommandTask: &CommandTask{
			Executable: executable,
			Args:       args,
		},
	}
}

func patternTask(id, name, category string, risk RiskLevel, directoryNames []string) CleanupTask {
	return CleanupTask{
		ID:          id,
		Kind:        TaskKindPattern,
		Name:        name,
		Category:    category,
		Description: "cleanup matched artifact directories under explicit roots",
		Risk:        risk,
		PatternTask: &PatternTask{
			Roots:          []string{},
			DirectoryNames: directoryNames,
		},
	}
}
