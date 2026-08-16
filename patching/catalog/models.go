package catalog

import "time"

const (
	StatusDraft     = "draft"
	StatusTesting   = "testing"
	StatusPublished = "published"
	StatusRetired   = "retired"
)

type Release struct {
	ID             uint64
	DivisionID     uint8
	ContentID      uint8
	ModuleID       uint8
	ModuleName     string
	BaseVersion    uint32
	Version        uint32
	DisplayVersion string
	Description    string
	Status         string
	CreatedAt      time.Time
	Files          []File
}

type File struct {
	ID             uint32 `json:"id"`
	Ordinal        uint16 `json:"-"`
	Filename       string `json:"filename"`
	Path           string `json:"path"`
	PreparedSize   uint64 `json:"size"`
	ImportToPK2    bool   `json:"import_to_pk2"`
	StorageKey     string `json:"storage_path"`
	PreparedSHA256 string `json:"prepared_sha256,omitempty"`
	TargetSize     uint64 `json:"target_size,omitempty"`
	TargetSHA256   string `json:"target_sha256,omitempty"`
}

type DownloadServer struct {
	Host string
	Port uint16
}

type PatchPlan struct {
	TargetVersion uint32
	Files         []File
	Server        DownloadServer
}
