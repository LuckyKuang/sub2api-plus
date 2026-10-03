package service

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

func ChannelMonitorV3Scope(platform string, groupID int64, model string) string {
	return fmt.Sprintf("%s:%d:%s", platform, groupID, model)
}

// ResumeChannelMonitorV3State discards confirmation evidence across a pause.
// Open incidents stay unresolved and wait for new observed requests.
func ResumeChannelMonitorV3State(previous ChannelMonitorV3State, now time.Time) ChannelMonitorV3State {
	next := previous
	next.Pending, next.Streak, next.PendingSince = "", 0, time.Time{}
	if previous.Incident != nil {
		incident := *previous.Incident
		incident.Updates = append([]ChannelMonitorV3Update(nil), incident.Updates...)
		next.Incident = &incident
		if incident.Phase != "awaiting_data" {
			updateV3Incident(next.Incident, "awaiting_data", incident.Severity, now)
		}
	}
	return next
}

// Advance requires new terminal requests, not merely a new timer tick. The same
// rolling samples cannot confirm a failure twice or manufacture recovery.
func AdvanceChannelMonitorV3State(previous ChannelMonitorV3State, fact ChannelMonitorV3Fact, now time.Time, cfg ChannelMonitorV3Config) (ChannelMonitorV3State, *ChannelMonitorV3Incident) {
	if !now.After(previous.LastEvaluated) {
		return previous, nil
	}
	next := previous
	next.LastEvaluated = now
	if previous.Incident != nil {
		copyIncident := *previous.Incident
		copyIncident.Updates = append([]ChannelMonitorV3Update(nil), previous.Incident.Updates...)
		next.Incident = &copyIncident
	}
	health := ChannelMonitorV3Health(fact, cfg)
	if health == "unknown" || health == "insufficient" || fact.LastRequest.Before(now.Add(-5*time.Minute)) {
		next.Pending, next.Streak = "", 0
		if next.Incident != nil && next.Incident.Phase != "awaiting_data" {
			updateV3Incident(next.Incident, "awaiting_data", next.Incident.Severity, now)
			return next, next.Incident
		}
		return next, nil
	}
	if !fact.LastRequest.After(previous.LastRequest) {
		return next, nil
	}
	next.LastRequest = fact.LastRequest
	if next.Pending == "" || (next.Pending == "normal") != (health == "normal") {
		next.Pending, next.Streak, next.PendingSince = health, 1, fact.LastRequest
	} else {
		next.Pending = health
		next.Streak++
	}
	if health == "normal" {
		if next.Incident == nil {
			return next, nil
		}
		if next.Streak >= cfg.RecoveryWindows {
			incident := next.Incident
			updateV3Incident(incident, "resolved", incident.Severity, now)
			incident.ResolvedAt = &now
			next.Incident = nil
			return next, incident
		}
		if next.Incident.Phase != "recovering" {
			updateV3Incident(next.Incident, "recovering", next.Incident.Severity, now)
			return next, next.Incident
		}
		return next, nil
	}
	if next.Streak < cfg.AbnormalWindows {
		// A bad request during recovery immediately revokes recovery evidence.
		if next.Incident != nil && (next.Incident.Phase == "recovering" || next.Incident.Phase == "awaiting_data") {
			updateV3Incident(next.Incident, "ongoing", next.Incident.Severity, now)
			return next, next.Incident
		}
		return next, nil
	}
	if next.Incident == nil {
		next.Incident = &ChannelMonitorV3Incident{ID: uuid.NewString(), Platform: fact.Platform, GroupID: fact.GroupID, Model: fact.Model, StartedAt: next.PendingSince}
		updateV3Incident(next.Incident, "detected", health, now)
		return next, next.Incident
	}
	if next.Incident.Severity != health || next.Incident.Phase == "recovering" || next.Incident.Phase == "awaiting_data" {
		updateV3Incident(next.Incident, "ongoing", health, now)
		return next, next.Incident
	}
	return next, nil
}

func updateV3Incident(event *ChannelMonitorV3Incident, phase, severity string, now time.Time) {
	event.Phase, event.Severity, event.UpdatedAt = phase, severity, now
	event.Updates = append(event.Updates, ChannelMonitorV3Update{Phase: phase, Severity: severity, At: now})
	if len(event.Updates) > 32 {
		event.Updates = append(event.Updates[:1:1], event.Updates[len(event.Updates)-31:]...)
	}
}
