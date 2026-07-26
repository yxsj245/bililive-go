package events

type EventType string

type EventHandler func(event *Event)

type Event struct {
	Type   EventType
	Object any
	// Source 标识事件的发布实例，供处理器拒绝已失效实例的迟到事件。
	Source any
}

func NewEvent(eventType EventType, object any) *Event {
	return &Event{Type: eventType, Object: object}
}

func NewEventWithSource(eventType EventType, object, source any) *Event {
	return &Event{Type: eventType, Object: object, Source: source}
}

type EventListener struct {
	Handler EventHandler
}

func NewEventListener(handler EventHandler) *EventListener {
	return &EventListener{handler}
}
