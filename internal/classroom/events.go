package classroom

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func (s *Service) emit(name string, payload any) error {
	if s.Bus == nil {
		return nil
	}
	_, err := s.Bus.Publish(types.EventType("classroom."+name), nil, nil, payload)
	return err
}
func (s *Service) courseEvent(name string, c Course) error {
	view := c.View()
	return s.emit(name, api.ClassroomCourseEvent{ClassroomEvent: api.ClassroomEvent{CourseID: c.ID}, Course: &view})
}
