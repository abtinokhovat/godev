package application

import (
	"fmt"
	"reflect"

	"github.com/abtinokhovat/godev/internal/config"
	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
)

// Reload re-reads this project's .godev.yaml and reconciles it against
// the services this Supervisor already knows about (see cmd/godev's
// ctrl+r): a name it hasn't seen before is added in Discovered state -
// Reload never starts it itself, regardless of its own auto_start
// setting, the same as every other service sits idle until something
// explicitly starts it; a known service whose definition actually
// changed gets its config replaced in place and, only if it's
// currently running, restarted with the new definition; a service
// whose definition is unchanged is left completely alone, so
// refreshing never interrupts services that didn't need it. A service
// removed from .godev.yaml is left as-is too - Reload only ever adds
// or updates, it never stops or drops a service out from under the
// user.
func (s *Supervisor) Reload() error {
	cfg, err := config.Load(s.ProjectRoot)
	if err != nil {
		wrapped := fmt.Errorf("loading %s: %w", config.FileName, err)
		s.log("", logs.StreamSystem, "reload failed: "+wrapped.Error())
		return wrapped
	}
	services, err := config.Merge(s.ProjectRoot, nil, cfg)
	if err != nil {
		s.log("", logs.StreamSystem, "reload failed: "+err.Error())
		return err
	}
	if len(services) == 0 {
		err := fmt.Errorf("no services found in %s", config.FileName)
		s.log("", logs.StreamSystem, "reload failed: "+err.Error())
		return err
	}

	var added, changed []string
	for _, svc := range services {
		e, exists := s.entry(svc.Name)
		if !exists {
			s.addService(svc)
			added = append(added, svc.Name)
			continue
		}

		s.mu.Lock()
		same := reflect.DeepEqual(e.svc, svc)
		if same {
			s.mu.Unlock()
			continue
		}
		e.svc = svc
		running := e.runtime.State == domain.StateRunning || e.runtime.State == domain.StateStarting
		s.mu.Unlock()

		changed = append(changed, svc.Name)
		s.events.Publish(Event{Type: EventServiceConfigChanged, Service: svc.Name, Message: "config changed on reload"})
		s.log(svc.Name, logs.StreamSystem, "config changed on reload")

		if running {
			go func(name string) {
				if err := s.Restart(name); err != nil {
					s.log(name, logs.StreamSystem, "restart after reload failed: "+err.Error())
				}
			}(svc.Name)
		}
	}

	s.log("", logs.StreamSystem, fmt.Sprintf("reloaded %s: %d new, %d changed", config.FileName, len(added), len(changed)))
	if len(added) > 0 || len(changed) > 0 {
		go s.rebuildDepIndex()
	}
	return nil
}

// addService registers a brand-new service in Discovered state
// without starting it - exactly how every service NewSupervisor
// itself populates starts out, before anything (StartAll, an explicit
// Start) decides to actually run it. Shared by NewSupervisor's initial
// population and Reload's "new name in .godev.yaml" case.
func (s *Supervisor) addService(svc domain.Service) {
	s.mu.Lock()
	s.entries[svc.Name] = &serviceEntry{
		svc:     svc,
		runtime: domain.ServiceRuntime{State: domain.StateDiscovered},
		backoff: newBackoff(),
	}
	s.order = append(s.order, svc.Name)
	s.mu.Unlock()
	s.events.Publish(Event{Type: EventServiceDiscovered, Service: svc.Name,
		Message: fmt.Sprintf("discovered %s (%s)", svc.Name, svc.Package)})
}
