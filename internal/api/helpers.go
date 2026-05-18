package api

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/iai/vibedeploy/internal/model"
)

// parseUUID is a checked wrapper that returns uuid.Nil on parse failure.
// Use only on UUIDs that already came out of the database (where we know the
// shape is valid); for user input use uuid.Parse and handle the error.
func parseUUID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}

// sprintTime renders a value scanned out of pgx (which yields time.Time or *time.Time)
// as an RFC3339 string. Returns empty for nil.
func sprintTime(v any) string {
	switch t := v.(type) {
	case time.Time:
		return t.Format(time.RFC3339)
	case *time.Time:
		if t == nil {
			return ""
		}
		return t.Format(time.RFC3339)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

// canManageProject — owner or admin can mutate collaborators / domains / settings.
// Collaborators with role=editor can push and read but not change membership.
func (s *Server) canManageProject(a Actor, p *model.Project) bool {
	switch a.Kind {
	case actorToken:
		// Platform-wide bootstrap tokens act as admin; project-scoped don't.
		return a.TokenProjectID == nil
	case actorUser:
		if a.IsAdmin {
			return true
		}
		return p.OwnerID == a.UserID
	}
	return false
}
