package kernel

type Event struct {
	ModuleID string
	Level    string
	Message  string
	Fields   map[string]string
}

type Reporter interface {
	Report(Event)
}

type ReporterFunc func(Event)

func (f ReporterFunc) Report(event Event) {
	if f != nil {
		f(event)
	}
}

func Report(reporter Reporter, event Event) {
	if reporter != nil {
		reporter.Report(event)
	}
}
