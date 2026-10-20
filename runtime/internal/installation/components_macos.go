//go:build linux || darwin

package installation

// Both architecture catalogs are tested on every build. macOS archives preserve
// their application bundle, including bounded framework symlinks and signatures.
func darwinBrowserRelease(arch string) (componentRelease, error) {
	r := componentRelease{Version: "0.39.0-chromium-149.0.7827.55"}
	switch arch {
	case "arm64":
		r.Assets = []componentAsset{
			{URL: "https://api.github.com/repos/vercel-labs/agent-browser/releases/assets/625880288", SHA256: "636fc9aa269e3819b539998aa5b87e1c3953556c19eeb9685053743283464faa", Format: "raw", Directory: "agent", Executable: "agent-browser", Size: 15803424, Expanded: 15803424},
			{URL: "https://storage.googleapis.com/chrome-for-testing-public/149.0.7827.55/mac-arm64/chrome-mac-arm64.zip", SHA256: "311211b54c429245e2cec0314ee1e314085e9c00350215b95e1a879350786630", Format: "zip", Prefix: "chrome-mac-arm64/", Directory: "chromium", Executable: "Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing", Size: 179277110, Expanded: 1 << 30, BundleLinks: true},
		}
	case "amd64":
		r.Assets = []componentAsset{
			{URL: "https://api.github.com/repos/vercel-labs/agent-browser/releases/assets/625880291", SHA256: "c080b1c2c4f03c2690778d72f30a0f8807cea1a5946b71e0ab6d71498f2d573e", Format: "raw", Directory: "agent", Executable: "agent-browser", Size: 17433384, Expanded: 17433384},
			{URL: "https://storage.googleapis.com/chrome-for-testing-public/149.0.7827.55/mac-x64/chrome-mac-x64.zip", SHA256: "4fff3b1bff4ab5acab495438d501fd56ecd326fc2e18670858930386dca864e6", Format: "zip", Prefix: "chrome-mac-x64/", Directory: "chromium", Executable: "Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing", Size: 189658978, Expanded: 1 << 30, BundleLinks: true},
		}
	default:
		return componentRelease{}, componentError("component_platform_unsupported", "no tested browser package for this platform")
	}
	return r, nil
}
