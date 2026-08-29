package version

// Version / Commit / Date 由 build.sh 在编译时通过 -ldflags -X 注入。
var (
	Version = "0.1.0"
	Commit  = "dev"
	Date    = "unknown"
)
