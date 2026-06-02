package db

import (
	"database/sql"
	"time"
)

type Session struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	EndpointURL       string         `json:"endpoint_url"`
	Model             string         `json:"model"`
	Owner             sql.NullString `json:"owner,omitempty"`
	RAG               bool           `json:"rag"`
	Archived          bool           `json:"archived"`
	Folder            sql.NullString `json:"folder,omitempty"`
	Headers           string         `json:"headers,omitempty"`
	LastAccessedAt    time.Time      `json:"last_accessed_at"`
	LastMessageAt     sql.NullTime   `json:"last_message_at,omitempty"`
	MessageCount      int            `json:"message_count"`
	IsImportant       bool           `json:"is_important"`
	Mode              sql.NullString `json:"mode,omitempty"`
	CrewMemberID      sql.NullString `json:"crew_member_id,omitempty"`
	TotalInputTokens  int            `json:"total_input_tokens"`
	TotalOutputTokens int            `json:"total_output_tokens"`
	SortOrder         int            `json:"sort_order"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

type ChatMessage struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	Metadata  sql.NullString `json:"metadata,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

type Document struct {
	ID                   string         `json:"id"`
	SessionID            sql.NullString `json:"session_id,omitempty"`
	Title                string         `json:"title"`
	Language             sql.NullString `json:"language,omitempty"`
	CurrentContent       string         `json:"current_content"`
	VersionCount         int            `json:"version_count"`
	IsActive             bool           `json:"is_active"`
	Archived             bool           `json:"archived"`
	Owner                sql.NullString `json:"owner,omitempty"`
	TidyVerdict          sql.NullString `json:"tidy_verdict,omitempty"`
	SourceEmailUID       sql.NullString `json:"source_email_uid,omitempty"`
	SourceEmailFolder    sql.NullString `json:"source_email_folder,omitempty"`
	SourceEmailAccountID sql.NullString `json:"source_email_account_id,omitempty"`
	SourceEmailMessageID sql.NullString `json:"source_email_message_id,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
}

type DocumentVersion struct {
	ID            string         `json:"id"`
	DocumentID    string         `json:"document_id"`
	VersionNumber int            `json:"version_number"`
	Content       string         `json:"content"`
	Summary       sql.NullString `json:"summary,omitempty"`
	Source        string         `json:"source"`
	CreatedAt     time.Time      `json:"created_at"`
}

type Memory struct {
	ID        string         `json:"id"`
	Text      string         `json:"text"`
	Category  string         `json:"category"`
	Source    string         `json:"source"`
	Owner     sql.NullString `json:"owner,omitempty"`
	SessionID sql.NullString `json:"session_id,omitempty"`
	Timestamp int64          `json:"timestamp"`
	Pinned    bool           `json:"pinned"`
}

type Note struct {
	ID        string         `json:"id"`
	Owner     sql.NullString `json:"owner,omitempty"`
	Title     string         `json:"title"`
	Content   sql.NullString `json:"content,omitempty"`
	Items     sql.NullString `json:"items,omitempty"`
	NoteType  string         `json:"note_type"`
	Color     sql.NullString `json:"color,omitempty"`
	Label     sql.NullString `json:"label,omitempty"`
	Pinned    bool           `json:"pinned"`
	Archived  bool           `json:"archived"`
	DueDate   sql.NullString `json:"due_date,omitempty"`
	Source    string         `json:"source"`
	SessionID sql.NullString `json:"session_id,omitempty"`
	ImageURL  sql.NullString `json:"image_url,omitempty"`
	Repeat    string         `json:"repeat"`
	SortOrder sql.NullInt64  `json:"sort_order,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type CalendarCal struct {
	ID        string         `json:"id"`
	Owner     sql.NullString `json:"owner,omitempty"`
	Name      string         `json:"name"`
	Color     string         `json:"color"`
	Source    string         `json:"source"`
	RemoteURL sql.NullString `json:"remote_url,omitempty"`
	IsVisible bool           `json:"is_visible"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type CalendarEvent struct {
	ID          string         `json:"id"`
	UID         string         `json:"uid"`
	CalendarID  string         `json:"calendar_id"`
	Title       string         `json:"title"`
	Description sql.NullString `json:"description,omitempty"`
	Location    sql.NullString `json:"location,omitempty"`
	StartTime   time.Time      `json:"start_time"`
	EndTime     time.Time      `json:"end_time"`
	AllDay      bool           `json:"all_day"`
	RRule       sql.NullString `json:"rrule,omitempty"`
	IsUTC       bool           `json:"is_utc"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type ModelEndpoint struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	BaseURL       string         `json:"base_url"`
	APIKey        sql.NullString `json:"api_key,omitempty"`
	IsEnabled     bool           `json:"is_enabled"`
	HiddenModels  sql.NullString `json:"hidden_models,omitempty"`
	CachedModels  sql.NullString `json:"cached_models,omitempty"`
	ModelType     string         `json:"model_type"`
	SupportsTools sql.NullBool   `json:"supports_tools,omitempty"`
	Owner         sql.NullString `json:"owner,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type McpServer struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Transport     string         `json:"transport"`
	Command       sql.NullString `json:"command,omitempty"`
	Args          sql.NullString `json:"args,omitempty"`
	Env           sql.NullString `json:"env,omitempty"`
	URL           sql.NullString `json:"url,omitempty"`
	IsEnabled     bool           `json:"is_enabled"`
	OAuthConfig   sql.NullString `json:"oauth_config,omitempty"`
	DisabledTools sql.NullString `json:"disabled_tools,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type EmailAccount struct {
	ID           string         `json:"id"`
	Owner        sql.NullString `json:"owner,omitempty"`
	Name         string         `json:"name"`
	IsDefault    bool           `json:"is_default"`
	Enabled      bool           `json:"enabled"`
	IMAPHost     string         `json:"imap_host"`
	IMAPPort     int            `json:"imap_port"`
	IMAPUser     string         `json:"imap_user"`
	IMAPPass     string         `json:"-"`
	IMAPStartTLS bool           `json:"imap_start_tls"`
	SMTPHost     string         `json:"smtp_host"`
	SMTPPort     int            `json:"smtp_port"`
	SMTPUser     string         `json:"smtp_user"`
	SMTPPass     string         `json:"-"`
	FromAddress  string         `json:"from_address"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type GalleryImage struct {
	ID          string         `json:"id"`
	Filename    string         `json:"filename"`
	Prompt      string         `json:"prompt"`
	Model       sql.NullString `json:"model,omitempty"`
	Size        sql.NullString `json:"size,omitempty"`
	Quality     sql.NullString `json:"quality,omitempty"`
	Tags        string         `json:"tags"`
	AITags      string         `json:"ai_tags"`
	SessionID   sql.NullString `json:"session_id,omitempty"`
	AlbumID     sql.NullString `json:"album_id,omitempty"`
	Owner       sql.NullString `json:"owner,omitempty"`
	IsActive    bool           `json:"is_active"`
	Favorite    bool           `json:"favorite"`
	FileHash    sql.NullString `json:"file_hash,omitempty"`
	TakenAt     sql.NullTime   `json:"taken_at,omitempty"`
	CameraMake  sql.NullString `json:"camera_make,omitempty"`
	CameraModel sql.NullString `json:"camera_model,omitempty"`
	GPSLat      sql.NullString `json:"gps_lat,omitempty"`
	GPSLng      sql.NullString `json:"gps_lng,omitempty"`
	Width       sql.NullInt64  `json:"width,omitempty"`
	Height      sql.NullInt64  `json:"height,omitempty"`
	FileSize    sql.NullInt64  `json:"file_size,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type GalleryAlbum struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	CoverID     sql.NullString `json:"cover_id,omitempty"`
	Owner       sql.NullString `json:"owner,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type APIToken struct {
	ID          string         `json:"id"`
	Owner       sql.NullString `json:"owner,omitempty"`
	Name        string         `json:"name"`
	TokenHash   string         `json:"-"`
	TokenPrefix string         `json:"token_prefix"`
	Scopes      string         `json:"scopes"`
	IsActive    bool           `json:"is_active"`
	LastUsedAt  sql.NullTime   `json:"last_used_at,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type Webhook struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	URL             string         `json:"url"`
	Secret          sql.NullString `json:"secret,omitempty"`
	Events          string         `json:"events"`
	IsActive        bool           `json:"is_active"`
	LastTriggeredAt sql.NullTime   `json:"last_triggered_at,omitempty"`
	LastStatusCode  sql.NullInt64  `json:"last_status_code,omitempty"`
	LastError       sql.NullString `json:"last_error,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type ScheduledTask struct {
	ID                   string         `json:"id"`
	Owner                sql.NullString `json:"owner,omitempty"`
	Name                 string         `json:"name"`
	Prompt               sql.NullString `json:"prompt,omitempty"`
	TaskType             string         `json:"task_type"`
	Action               sql.NullString `json:"action,omitempty"`
	Schedule             sql.NullString `json:"schedule,omitempty"`
	ScheduledTime        sql.NullString `json:"scheduled_time,omitempty"`
	ScheduledDay         sql.NullInt64  `json:"scheduled_day,omitempty"`
	ScheduledDate        sql.NullTime   `json:"scheduled_date,omitempty"`
	TriggerType          string         `json:"trigger_type"`
	TriggerEvent         sql.NullString `json:"trigger_event,omitempty"`
	TriggerCount         sql.NullInt64  `json:"trigger_count,omitempty"`
	TriggerCounter       int            `json:"trigger_counter"`
	NextRun              sql.NullTime   `json:"next_run,omitempty"`
	LastRun              sql.NullTime   `json:"last_run,omitempty"`
	Status               string         `json:"status"`
	OutputTarget         string         `json:"output_target"`
	SessionID            sql.NullString `json:"session_id,omitempty"`
	Model                sql.NullString `json:"model,omitempty"`
	EndpointURL          sql.NullString `json:"endpoint_url,omitempty"`
	RunCount             int            `json:"run_count"`
	CronExpression       sql.NullString `json:"cron_expression,omitempty"`
	ThenTaskID           sql.NullString `json:"then_task_id,omitempty"`
	WebhookToken         sql.NullString `json:"webhook_token,omitempty"`
	CrewMemberID         sql.NullString `json:"crew_member_id,omitempty"`
	MaxSteps             sql.NullInt64  `json:"max_steps,omitempty"`
	EmailResults         bool           `json:"email_results"`
	NotificationsEnabled bool           `json:"notifications_enabled"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
}

type TaskRun struct {
	ID         string         `json:"id"`
	TaskID     string         `json:"task_id"`
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt sql.NullTime   `json:"finished_at,omitempty"`
	Status     string         `json:"status"`
	Result     sql.NullString `json:"result,omitempty"`
	Error      sql.NullString `json:"error,omitempty"`
	TokensUsed sql.NullInt64  `json:"tokens_used,omitempty"`
	Steps      sql.NullString `json:"steps,omitempty"`
	Model      sql.NullString `json:"model,omitempty"`
}

type EditorDraft struct {
	ID            string         `json:"id"`
	Owner         sql.NullString `json:"owner,omitempty"`
	Name          string         `json:"name"`
	SourceImageID sql.NullString `json:"source_image_id,omitempty"`
	Width         sql.NullInt64  `json:"width,omitempty"`
	Height        sql.NullInt64  `json:"height,omitempty"`
	Payload       string         `json:"payload"`
	Thumbnail     sql.NullString `json:"thumbnail,omitempty"`
	IsActive      bool           `json:"is_active"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type Comparison struct {
	ID          string         `json:"id"`
	SessionID   sql.NullString `json:"session_id,omitempty"`
	Owner       sql.NullString `json:"owner,omitempty"`
	Prompt      string         `json:"prompt"`
	ModelA      string         `json:"model_a"`
	ModelB      string         `json:"model_b"`
	EndpointA   string         `json:"endpoint_a"`
	EndpointB   string         `json:"endpoint_b"`
	ResponseA   sql.NullString `json:"response_a,omitempty"`
	ResponseB   sql.NullString `json:"response_b,omitempty"`
	MetricsA    sql.NullString `json:"metrics_a,omitempty"`
	MetricsB    sql.NullString `json:"metrics_b,omitempty"`
	Winner      sql.NullString `json:"winner,omitempty"`
	IsBlind     bool           `json:"is_blind"`
	BlindMapping sql.NullString `json:"blind_mapping,omitempty"`
	VotedAt     sql.NullTime   `json:"voted_at,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}
