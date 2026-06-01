package db

import (
	"database/sql"
	"time"
)

type Session struct {
	ID                string
	Name              string
	EndpointURL       string
	Model             string
	Owner             sql.NullString
	RAG               bool
	Archived          bool
	Folder            sql.NullString
	Headers           string // JSON
	LastAccessedAt    time.Time
	LastMessageAt     sql.NullTime
	MessageCount      int
	IsImportant       bool
	Mode              sql.NullString
	CrewMemberID      sql.NullString
	TotalInputTokens  int
	TotalOutputTokens int
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ChatMessage struct {
	ID        string
	SessionID string
	Role      string
	Content   string
	Metadata  sql.NullString
	Timestamp time.Time
}

type Document struct {
	ID                   string
	SessionID            sql.NullString
	Title                string
	Language             sql.NullString
	CurrentContent       string
	VersionCount         int
	IsActive             bool
	Archived             bool
	Owner                sql.NullString
	TidyVerdict          sql.NullString
	SourceEmailUID       sql.NullString
	SourceEmailFolder    sql.NullString
	SourceEmailAccountID sql.NullString
	SourceEmailMessageID sql.NullString
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type DocumentVersion struct {
	ID            string
	DocumentID    string
	VersionNumber int
	Content       string
	Summary       sql.NullString
	Source        string
	CreatedAt     time.Time
}

type Memory struct {
	ID        string
	Text      string
	Category  string
	Source    string
	Owner     sql.NullString
	SessionID sql.NullString
	Timestamp int64
}

type Note struct {
	ID        string
	Owner     sql.NullString
	Title     string
	Content   sql.NullString
	Items     sql.NullString // JSON
	NoteType  string
	Color     sql.NullString
	Label     sql.NullString
	Pinned    bool
	Archived  bool
	DueDate   sql.NullString
	Source    string
	SessionID sql.NullString
	ImageURL  sql.NullString
	Repeat    string
	SortOrder sql.NullInt64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CalendarCal struct {
	ID        string
	Owner     sql.NullString
	Name      string
	Color     string
	Source    string
	RemoteURL sql.NullString
	IsVisible bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CalendarEvent struct {
	ID          string
	UID         string
	CalendarID  string
	Title       string
	Description sql.NullString
	Location    sql.NullString
	StartTime   time.Time
	EndTime     time.Time
	AllDay      bool
	RRule       sql.NullString
	IsUTC       bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ModelEndpoint struct {
	ID            string
	Name          string
	BaseURL       string
	APIKey        sql.NullString // encrypted at rest
	IsEnabled     bool
	HiddenModels  sql.NullString // JSON array
	CachedModels  sql.NullString // JSON array
	ModelType     string
	SupportsTools sql.NullBool
	Owner         sql.NullString
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type McpServer struct {
	ID            string
	Name          string
	Transport     string
	Command       sql.NullString
	Args          sql.NullString // JSON array
	Env           sql.NullString // JSON object
	URL           sql.NullString
	IsEnabled     bool
	OAuthConfig   sql.NullString // JSON
	DisabledTools sql.NullString // JSON array
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type EmailAccount struct {
	ID          string
	Owner       sql.NullString
	Name        string
	IsDefault   bool
	Enabled     bool
	IMAPHost    string
	IMAPPort    int
	IMAPUser    string
	IMAPPass    string // encrypted at rest
	IMAPStartTLS bool
	SMTPHost    string
	SMTPPort    int
	SMTPUser    string
	SMTPPass    string // encrypted at rest
	FromAddress string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type GalleryImage struct {
	ID          string
	Filename    string
	Prompt      string
	Model       sql.NullString
	Size        sql.NullString
	Quality     sql.NullString
	Tags        string
	AITags      string
	SessionID   sql.NullString
	AlbumID     sql.NullString
	Owner       sql.NullString
	IsActive    bool
	Favorite    bool
	FileHash    sql.NullString
	TakenAt     sql.NullTime
	CameraMake  sql.NullString
	CameraModel sql.NullString
	GPSLat      sql.NullString
	GPSLng      sql.NullString
	Width       sql.NullInt64
	Height      sql.NullInt64
	FileSize    sql.NullInt64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type GalleryAlbum struct {
	ID          string
	Name        string
	Description string
	CoverID     sql.NullString
	Owner       sql.NullString
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type APIToken struct {
	ID          string
	Owner       sql.NullString
	Name        string
	TokenHash   string
	TokenPrefix string
	Scopes      string
	IsActive    bool
	LastUsedAt  sql.NullTime
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Webhook struct {
	ID              string
	Name            string
	URL             string
	Secret          sql.NullString
	Events          string
	IsActive        bool
	LastTriggeredAt sql.NullTime
	LastStatusCode  sql.NullInt64
	LastError       sql.NullString
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ScheduledTask struct {
	ID                   string
	Owner                sql.NullString
	Name                 string
	Prompt               sql.NullString
	TaskType             string
	Action               sql.NullString
	Schedule             sql.NullString
	ScheduledTime        sql.NullString
	ScheduledDay         sql.NullInt64
	ScheduledDate        sql.NullTime
	TriggerType          string
	TriggerEvent         sql.NullString
	TriggerCount         sql.NullInt64
	TriggerCounter       int
	NextRun              sql.NullTime
	LastRun              sql.NullTime
	Status               string
	OutputTarget         string
	SessionID            sql.NullString
	Model                sql.NullString
	EndpointURL          sql.NullString
	RunCount             int
	CronExpression       sql.NullString
	ThenTaskID           sql.NullString
	WebhookToken         sql.NullString
	CrewMemberID         sql.NullString
	MaxSteps             sql.NullInt64
	EmailResults         bool
	NotificationsEnabled bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type TaskRun struct {
	ID         string
	TaskID     string
	StartedAt  time.Time
	FinishedAt sql.NullTime
	Status     string
	Result     sql.NullString
	Error      sql.NullString
	TokensUsed sql.NullInt64
	Steps      sql.NullString
	Model      sql.NullString
}

type EditorDraft struct {
	ID            string
	Owner         sql.NullString
	Name          string
	SourceImageID sql.NullString
	Width         sql.NullInt64
	Height        sql.NullInt64
	Payload       string
	Thumbnail     sql.NullString
	IsActive      bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Comparison struct {
	ID           string
	SessionID    sql.NullString
	Owner        sql.NullString
	Prompt       string
	ModelA       string
	ModelB       string
	EndpointA    string
	EndpointB    string
	ResponseA    sql.NullString
	ResponseB    sql.NullString
	MetricsA     sql.NullString
	MetricsB     sql.NullString
	Winner       sql.NullString
	IsBlind      bool
	BlindMapping sql.NullString
	VotedAt      sql.NullTime
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
