package preferences

import (
	"fmt"
	"path/filepath"
	"sync"
)

type Service struct {
	mutex     sync.RWMutex
	directory string
	current   Layout
}

func OpenDefault() (*Service, error) {
	directory, err := ResolveConfigDir()
	if err != nil {
		return nil, err
	}
	return Open(directory)
}

func Open(directory string) (*Service, error) {
	if !filepath.IsAbs(directory) {
		return nil, fmt.Errorf("presentation configuration directory must be absolute")
	}
	directory = filepath.Clean(directory)
	current, err := load(directory)
	if err != nil {
		return nil, err
	}
	return &Service{directory: directory, current: current}, nil
}

func (service *Service) Current() Layout {
	if service == nil {
		return Defaults()
	}
	service.mutex.RLock()
	defer service.mutex.RUnlock()
	return service.current
}

func (service *Service) SetSidebarVisible(visible bool) error {
	return service.update(func(layout *Layout) { layout.Sidebar.Visible = visible })
}

func (service *Service) SetSidebarSection(section string, visible bool) error {
	switch section {
	case "identity", "context", "provider", "status":
	default:
		return fmt.Errorf("unknown sidebar section %q", section)
	}
	return service.update(func(layout *Layout) {
		switch section {
		case "identity":
			layout.Sidebar.Identity = visible
		case "context":
			layout.Sidebar.Context = visible
		case "provider":
			layout.Sidebar.Provider = visible
		case "status":
			layout.Sidebar.Status = visible
		}
	})
}

func (service *Service) SetColor(role string, color Color) error {
	switch role {
	case "accent", "border", "background", "text":
	default:
		return fmt.Errorf("unknown presentation color role %q", role)
	}
	return service.update(func(layout *Layout) {
		switch role {
		case "accent":
			layout.Colors.Accent = color
		case "border":
			layout.Colors.Border = color
		case "background":
			layout.Colors.Background = color
		case "text":
			layout.Colors.Text = color
		}
	})
}

func (service *Service) Reset() error {
	if service == nil {
		return fmt.Errorf("presentation preferences are not configured")
	}
	service.mutex.Lock()
	defer service.mutex.Unlock()
	candidate := Defaults()
	if err := save(service.directory, candidate); err != nil {
		return err
	}
	service.current = candidate
	return nil
}

func (service *Service) ConfigPath() string {
	if service == nil {
		return ""
	}
	return Path(service.directory)
}

func (service *Service) update(change func(*Layout)) error {
	if service == nil {
		return fmt.Errorf("presentation preferences are not configured")
	}
	service.mutex.Lock()
	defer service.mutex.Unlock()
	candidate := service.current
	change(&candidate)
	if err := candidate.Validate(); err != nil {
		return err
	}
	if err := save(service.directory, candidate); err != nil {
		return err
	}
	service.current = candidate
	return nil
}
