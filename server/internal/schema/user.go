package schema

import (
	"time"

	"private-buddy-server/internal/model"
)

type UserProfileResponse struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Bio       string    `json:"bio"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UserProfileUpdate allows updating mutable user fields. Name is immutable.
type UserProfileUpdate struct {
	Bio *string `json:"bio"`
}

func NewUserProfileResponse(m *model.User) *UserProfileResponse {
	return &UserProfileResponse{
		ID:        m.ID,
		Name:      m.Name,
		Bio:       m.Bio,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}

func (req *UserProfileUpdate) BuildUpdates() map[string]interface{} {
	updates := make(map[string]interface{})
	if req.Bio != nil {
		updates["bio"] = *req.Bio
	}
	return updates
}
