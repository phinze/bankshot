package version

// These variables are set via ldflags during build
var (
	// Version is the semantic version of the application
	Version = "dev"
	
	// Commit is the git commit hash
	Commit = "none"
	
	// Date is the build date
	Date = "unknown"
	
	// BuiltBy indicates what triggered the build (e.g., goreleaser, make, go build)
	BuiltBy = "unknown"
)

// GetVersion returns a formatted version string
func GetVersion() string {
	return Version
}

// GetFullVersion returns the complete version information
func GetFullVersion() string {
	return Version + " (commit: " + Commit + ", built: " + Date + ", by: " + BuiltBy + ")"
}