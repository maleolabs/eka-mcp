module github.com/maleolabs/eka-mcp

go 1.24.0

require github.com/maleolabs/eka-core v1.2.1-0.20260816205231-6f9ee7f02205

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/exp v0.0.0-20251023183803-a4bb9ffd2546 // indirect
	golang.org/x/sys v0.37.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	modernc.org/libc v1.67.6 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	modernc.org/sqlite v1.45.0 // indirect
)

// Development replace (sto:mcp-contract-relocation): eka-core's plugin
// contract package (github.com/maleolabs/eka-core/plugin) is not yet
// merged to develop. The orchestrator merges eka-core first; this
// replace is then dropped in a follow-up bump to the published version.
replace github.com/maleolabs/eka-core => /home/m2codeloan/m2code/maleolabs/eka/worktrees/eka-core-feat-sto-mcp-contract-relocation
