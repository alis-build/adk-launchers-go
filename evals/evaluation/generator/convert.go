// Package generator converts ADK session events to eval invocations and runs inference.
package generator

import (
	"strings"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"google.golang.org/adk/v2/session"
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
			if event.GroundingMetadata != nil || event.UsageMetadata != nil || eventHasRelevantPart(event) {
				eventsToAdd = append(eventsToAdd, event)
			}
		} else if event.GroundingMetadata != nil || event.UsageMetadata != nil {
			eventsToAdd = append(eventsToAdd, event)
		}
	}

	mergedUsage, mergedReports := mergeUsageReports(eventsToAdd)
	invEvents := make([]models.InvocationEvent, 0, len(eventsToAdd))
	for i, e := range eventsToAdd {
		if mergedReports[i] {
			continue
		}
		usage := e.UsageMetadata
		if u, ok := mergedUsage[i]; ok {
			usage = u
		}
		content := e.Content
		// The final response is kept, as in adk-python, so its model call's
		// usage and grounding are not lost, but without its text: that is
		// already Invocation.FinalResponse and repeating it would count it
		// twice. Function calls stay, since tool trajectories read them here.
		if e == finalEvent && !eventHasFunctionCall(e) {
			content = nil
		}
		invEvents = append(invEvents, models.InvocationEvent{
			Author:            e.Author,
			Content:           content,
			GroundingMetadata: e.GroundingMetadata,
			UsageMetadata:     usage,
			ModelVersion:      e.ModelVersion,
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

// mergeUsageReports pairs each standalone usage report (an event with usage
// but no content and no grounding, as a live connection sends) with the model
// event it belongs to, so its tokens count without the report counting as a
// second model call (adk-python 8cd3b12c8). Nothing in a report names its
// call, so the nearest unclaimed compatible event wins, earlier ones first:
// same author, no usage of its own, a model version, and the report's model
// version when it has one. It returns the usage to put on each claimed
// event and the reports merged away; an unmatched report stays an event.
// ADK Go events carry no live-session ID, so adk-python's same-connection
// rule is not applied.
func mergeUsageReports(events []*session.Event) (map[int]*genai.GenerateContentResponseUsageMetadata, map[int]bool) {
	merged := map[int]*genai.GenerateContentResponseUsageMetadata{}
	reports := map[int]bool{}
	compatible := func(report, c *session.Event, ci int) bool {
		if _, claimed := merged[ci]; claimed {
			return false
		}
		return c.UsageMetadata == nil && c.ModelVersion != "" && c.Author == report.Author &&
			(report.ModelVersion == "" || c.ModelVersion == report.ModelVersion)
	}
	for i, e := range events {
		if (e.Content != nil && len(e.Content.Parts) > 0) || e.GroundingMetadata != nil || e.UsageMetadata == nil {
			continue
		}
		target := -1
		for ci := i - 1; ci >= 0 && target < 0; ci-- {
			if compatible(e, events[ci], ci) {
				target = ci
			}
		}
		for ci := i + 1; ci < len(events) && target < 0; ci++ {
			if compatible(e, events[ci], ci) {
				target = ci
			}
		}
		if target >= 0 {
			merged[target] = e.UsageMetadata
			reports[i] = true
		}
	}
	return merged, reports
}
