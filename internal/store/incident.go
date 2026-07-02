package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrStale = errors.New("incident version is stale")

type Incident struct {
	DedupKey       string
	Channel        string
	ChannelID      string
	Severity       string
	Status         string // active | resolved
	Version        int
	Title          string
	Body           string
	Host           string
	Link           string
	Labels         map[string]string
	DesiredPresent bool
	ContentHash    string
	MessageID      string
	StaleMessageID string
	CreatedAt      time.Time
	LastSeenFiring time.Time
	Confirmed      bool
	Heartbeat      bool
	AckedAt        *time.Time
	AckedBy        *string
	SnoozedUntil   *time.Time
	LastNotifiedAt *time.Time
	ResolvedAt     *time.Time
}

const cols = `dedup_key,channel,channel_id,severity,status,version,title,body,host,link,labels,
desired_present,content_hash,message_id,stale_message_id,created_at,last_seen_firing,confirmed,heartbeat,
acked_at,acked_by,snoozed_until,last_notified_at,resolved_at`

// b2i converts bool to int (1 for true, 0 for false)
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// unixOrZero returns the unix timestamp of t, or 0 if t is zero
func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// unixPtr returns a pointer to the unix timestamp of t, or nil if t is nil
func unixPtr(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	unix := t.Unix()
	return &unix
}

// nullTime converts a sql.NullInt64 to *time.Time, or nil if not valid
func nullTime(n sql.NullInt64) *time.Time {
	if !n.Valid || n.Int64 == 0 {
		return nil
	}
	t := time.Unix(n.Int64, 0)
	return &t
}

func (s *Store) Insert(in *Incident) error {
	labels, _ := json.Marshal(in.Labels)
	_, err := s.db.Exec(`insert into incidents
        (dedup_key,channel,channel_id,severity,status,version,title,body,host,link,labels,
         desired_present,content_hash,message_id,stale_message_id,created_at,last_seen_firing,confirmed,heartbeat)
        values (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		in.DedupKey, in.Channel, in.ChannelID, in.Severity, in.Status, in.Version,
		in.Title, in.Body, in.Host, in.Link, string(labels),
		b2i(in.DesiredPresent), in.ContentHash, in.MessageID, in.StaleMessageID,
		in.CreatedAt.Unix(), unixOrZero(in.LastSeenFiring), b2i(in.Confirmed), b2i(in.Heartbeat))
	if err != nil {
		return fmt.Errorf("insert incident %s: %w", in.DedupKey, err)
	}
	return nil
}

// Update writes the row only if the stored version still matches in.Version,
// then bumps in.Version. a concurrent writer that raced loses with ErrStale.
func (s *Store) Update(in *Incident) error {
	labels, _ := json.Marshal(in.Labels)
	res, err := s.db.Exec(`update incidents set
        channel=?,channel_id=?,severity=?,status=?,version=version+1,title=?,body=?,host=?,link=?,labels=?,
        desired_present=?,content_hash=?,message_id=?,stale_message_id=?,last_seen_firing=?,confirmed=?,heartbeat=?,
        acked_at=?,acked_by=?,snoozed_until=?,last_notified_at=?,resolved_at=?
        where dedup_key=? and version=?`,
		in.Channel, in.ChannelID, in.Severity, in.Status, in.Title, in.Body, in.Host, in.Link, string(labels),
		b2i(in.DesiredPresent), in.ContentHash, in.MessageID, in.StaleMessageID, unixOrZero(in.LastSeenFiring), b2i(in.Confirmed), b2i(in.Heartbeat),
		unixPtr(in.AckedAt), in.AckedBy, unixPtr(in.SnoozedUntil), unixPtr(in.LastNotifiedAt), unixPtr(in.ResolvedAt),
		in.DedupKey, in.Version)
	if err != nil {
		return fmt.Errorf("update incident %s: %w", in.DedupKey, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrStale
	}
	in.Version++
	return nil
}

func (s *Store) ActiveByKey(key string) (*Incident, error) {
	return s.scanOne(`select `+cols+` from incidents where dedup_key=? and status='active'`, key)
}

// DueForReap returns active, heartbeat-backed incidents last seen firing before cutoff.
func (s *Store) DueForReap(cutoff time.Time) ([]*Incident, error) {
	return s.scanMany(`select `+cols+` from incidents
        where status='active' and heartbeat=1 and last_seen_firing>0 and last_seen_firing<?`,
		cutoff.Unix())
}

// DueForRenotify returns active, unacked incidents with a live message, no repost
// already staged, whose snooze (if any) has lapsed and whose last notify is older
// than cutoff.
func (s *Store) DueForRenotify(now, cutoff time.Time) ([]*Incident, error) {
	return s.scanMany(`select `+cols+` from incidents
        where status='active' and acked_at is null and message_id<>'' and stale_message_id=''
        and (snoozed_until is null or snoozed_until<=?)
        and (last_notified_at is null or last_notified_at<=?)`,
		now.Unix(), cutoff.Unix())
}

// NeedingConverge returns rows whose confirmed discord state may differ from desired.
func (s *Store) NeedingConverge() ([]*Incident, error) {
	return s.scanMany(`select ` + cols + ` from incidents
        where confirmed=0 or stale_message_id<>'' or (desired_present=0 and message_id<>'')`)
}

// ByKey returns the incident regardless of status (the converger needs resolved rows).
func (s *Store) ByKey(key string) (*Incident, error) {
	return s.scanOne(`select `+cols+` from incidents where dedup_key=?`, key)
}

func (s *Store) MarkAllUnconfirmed() error {
	_, err := s.db.Exec(`update incidents set confirmed=0, version=version+1 where status='active'`)
	return err
}

func (s *Store) scanOne(query string, args ...any) (*Incident, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	return scan(rows)
}

func (s *Store) scanMany(query string, args ...any) ([]*Incident, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var incidents []*Incident
	for rows.Next() {
		in, err := scan(rows)
		if err != nil {
			return nil, err
		}
		incidents = append(incidents, in)
	}
	return incidents, rows.Err()
}

func scan(rows *sql.Rows) (*Incident, error) {
	var in Incident
	var labels string
	var desired, confirmed, heartbeat int
	var created, lastFiring int64
	var acked, snoozed, lastNotified, resolved sql.NullInt64
	var ackedBy sql.NullString
	if err := rows.Scan(&in.DedupKey, &in.Channel, &in.ChannelID, &in.Severity, &in.Status, &in.Version,
		&in.Title, &in.Body, &in.Host, &in.Link, &labels, &desired, &in.ContentHash, &in.MessageID, &in.StaleMessageID,
		&created, &lastFiring, &confirmed, &heartbeat, &acked, &ackedBy, &snoozed, &lastNotified, &resolved); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(labels), &in.Labels)
	in.DesiredPresent = desired == 1
	in.Confirmed = confirmed == 1
	in.Heartbeat = heartbeat == 1
	in.CreatedAt = time.Unix(created, 0)
	if lastFiring > 0 {
		in.LastSeenFiring = time.Unix(lastFiring, 0)
	}
	in.AckedAt = nullTime(acked)
	in.SnoozedUntil = nullTime(snoozed)
	in.LastNotifiedAt = nullTime(lastNotified)
	in.ResolvedAt = nullTime(resolved)
	if ackedBy.Valid {
		in.AckedBy = &ackedBy.String
	}
	return &in, nil
}
