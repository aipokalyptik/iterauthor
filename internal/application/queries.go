package application

import "github.com/aipokalyptik/iterauthor/internal/project"

func read[T any](s *Service, fn func(*project.Store) (T, error)) (T, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.available(); err != nil {
		var zero T
		return zero, err
	}
	return fn(s.store)
}
func (s *Service) Read(id, field string) (string, error) {
	return read(s, func(p *project.Store) (string, error) { return p.Read(id, field) })
}
func (s *Service) Snapshot() (project.Snapshot, error) {
	return read(s, func(p *project.Store) (project.Snapshot, error) { return p.Snapshot() })
}
func (s *Service) Runs() ([]project.Run, error) {
	return read(s, func(p *project.Store) ([]project.Run, error) { return p.Runs() })
}
func (s *Service) LoadRun(id string) (project.Run, error) {
	return read(s, func(p *project.Store) (project.Run, error) { return p.LoadRun(id) })
}
func (s *Service) Conversations() ([]project.Conversation, error) {
	return read(s, func(p *project.Store) ([]project.Conversation, error) { return p.Conversations() })
}
func (s *Service) LoadConversation(id string) (project.Conversation, error) {
	return read(s, func(p *project.Store) (project.Conversation, error) { return p.LoadConversation(id) })
}
func (s *Service) Status(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return "Closed"
	}
	return s.store.Status(id)
}
func (s *Service) Manuscript() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return "Project closed"
	}
	return s.store.Manuscript()
}
func (s *Service) Export() (string, error) {
	return read(s, func(p *project.Store) (string, error) { return p.Export() })
}
func (s *Service) SourceHistory(id string) ([]project.SourceVersion, error) {
	return read(s, func(p *project.Store) ([]project.SourceVersion, error) { return p.SourceHistory(id) })
}
func (s *Service) ReadSourceVersion(id string) (string, error) {
	return read(s, func(p *project.Store) (string, error) { return p.ReadSourceVersion(id) })
}
