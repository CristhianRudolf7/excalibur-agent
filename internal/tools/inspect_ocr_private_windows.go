package tools

import "github.com/CristhianRudolf7/excalibur-agent/internal/platform"

func restrictOCRFile(path string) error { return platform.RestrictToOwner(path) }
