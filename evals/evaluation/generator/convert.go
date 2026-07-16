// Package generator converts ADK session events to eval invocations and runs inference.
package generator

import (
	"strings"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

const (
	userAuthor    = "user"
	defaultAuthor = "agent"
)

// ConvertEventsToEvalInvocations maps session events to golden/eval invocations.
func ConvertEventsToEvalInvocations(events []*session.Event, appDetailsByInvocation map[string]*models.AppDetails) []models.Invocation {
	grouped := CollectEventsByInvocationID(events)
	invocations := make([]models.Invocation, 0, len(grouped))
	for _, invocationID := range invocationOrder(events) {
		invEvents := grouped[invocationID]
		invocations = append(invocations, convertInvocationEvents(invocationID, invEvents, appDetailsByInvocation))
	}
	return invocations
}

// CollectEventsByInvocationID groups events by InvocationID.
func CollectEventsByInvocationID(events []*session.Event) map[string][]*session.Event {
	out := make(map[string][]*session.Event)
	for _, ev := range events {
		if ev == nil {
			continue
		}
		out[ev.InvocationID] = append(out[ev.InvocationID], ev)
	}
	return out
}

// invocationOrder returns invocation IDs in first-seen event order.
func invocationOrder(events []*session.Event) []string {
	seen := make(map[string]struct{})
	order := make([]string, 0)
	for _, ev := range events {
		if ev == nil {
			continue
		}
		if _, ok := seen[ev.InvocationID]; ok {
			continue
		}
		seen[ev.InvocationID] = struct{}{}
		order = append(order, ev.InvocationID)
	}
	return order
}

// convertInvocationEvents projects session events into one eval Invocation.
func convertInvocationEvents(invocationID string, events []*session.Event, appDetailsByInvocation map[string]*models.AppDetails) models.Invocation {
	var userContent *genai.Content
	var finalResponse *genai.Content
	var finalEvent *session.Event
	var invocationTimestamp float64
	var appDetails *models.AppDetails

	if appDetailsByInvocation != nil {
		appDetails = appDetailsByInvocation[invocationID]
	}

	var eventsToAdd []*session.Event
	for _, event := range events {
		author := event.Author
		if author == "" {
			author = defaultAuthor
		}
		if strings.EqualFold(author, userAuthor) {
			if event.Content != nil {
				userContent = event.Content
				invocationTimestamp = float64(event.Timestamp.Unix())
			}
			continue
		}
		if event.Content != nil && len(event.Content.Parts) > 0 {
			if event.IsFinalResponse() {
				finalResponse = event.Content
				finalEvent = event
			}
			if eventHasRelevantPart(event) {
				eventsToAdd = append(eventsToAdd, event)
			}
		}
	}

	invEvents := make([]models.InvocationEvent, 0, len(eventsToAdd))
	for _, e := range eventsToAdd {
		if finalEvent != nil && e == finalEvent && !eventHasFunctionCall(e) {
			continue
		}
		invEvents = append(invEvents, models.InvocationEvent{
			Author:  e.Author,
			Content: e.Content,
		})
	}

	if userContent == nil {
		userContent = &genai.Content{Parts: []*genai.Part{}}
	}

	return models.Invocation{
		InvocationID:  invocationID,
		UserContent:   userContent,
		FinalResponse: finalResponse,
		IntermediateData: models.InvocationEventsField(models.InvocationEvents{
			InvocationEvents: invEvents,
		}),
		CreationTimestamp: invocationTimestamp,
		AppDetails:        appDetails,
	}
}

// eventHasRelevantPart reports whether the event carries tool or text content
// worth storing in intermediate invocation events.
func eventHasRelevantPart(event *session.Event) bool {
	if event.Content == nil {
		return false
	}
	for _, p := range event.Content.Parts {
		if p == nil {
			continue
		}
		if p.FunctionCall != nil || p.FunctionResponse != nil || p.Text != "" || p.InlineData != nil {
			return true
		}
	}
	return false
}

// eventHasFunctionCall reports whether the event includes a function call part.
func eventHasFunctionCall(event *session.Event) bool {
	if event.Content == nil {
		return false
	}
	for _, p := range event.Content.Parts {
		if p != nil && p.FunctionCall != nil {
			return true
		}
	}
	return false
}
