package install

type StepEvent struct {
	StepID    string
	Kind      string
	Phase     string
	Status    string
	Reason    string
	Attempt   int
	StartedAt string
	EndedAt   string
	Error     string
}

type StepEventSink func(StepEvent)

func emitStepEvent(sink StepEventSink, event StepEvent) {
	if sink == nil {
		return
	}
	sink(event)
}
