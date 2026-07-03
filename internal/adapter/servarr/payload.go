package servarr

// payload is the union of the servarr Connect webhook fields this adapter reads.
// one POST is one event; the relevant fields depend on eventType. keys are
// camelCase; eventType is PascalCase; null fields are omitted by servarr.
type payload struct {
	EventType      string `json:"eventType"`
	InstanceName   string `json:"instanceName"`
	ApplicationURL string `json:"applicationUrl"`

	// the Health and HealthRestored events
	Level   string `json:"level"`
	Message string `json:"message"`
	Type    string `json:"type"`
	WikiURL string `json:"wikiUrl"`

	// the ManualInteractionRequired and Download events (radarr, sonarr)
	DownloadID             string        `json:"downloadId"`
	DownloadClient         string        `json:"downloadClient"`
	DownloadStatus         string        `json:"downloadStatus"`
	DownloadStatusMessages []statusMsg   `json:"downloadStatusMessages"`
	DownloadInfo           *downloadInfo `json:"downloadInfo"`
	Release                *release      `json:"release"`
	Movie                  *named        `json:"movie"`
	Series                 *named        `json:"series"`

	// lidarr DownloadFailure / ImportFailure
	Artist       *named      `json:"artist"`
	Quality      string      `json:"quality"`
	ReleaseTitle string      `json:"releaseTitle"`
	TrackFiles   []trackFile `json:"trackFiles"`
}

type named struct {
	Title string `json:"title"` // movie/series
	Name  string `json:"name"`  // artist
}

type statusMsg struct {
	Title    string   `json:"title"`
	Messages []string `json:"messages"`
}

type downloadInfo struct {
	Quality string `json:"quality"`
	Size    int64  `json:"size"`
}

type release struct {
	ReleaseTitle string `json:"releaseTitle"`
	Indexer      string `json:"indexer"`
	Size         int64  `json:"size"`
}

type trackFile struct {
	Quality string `json:"quality"`
	Size    int64  `json:"size"`
}
