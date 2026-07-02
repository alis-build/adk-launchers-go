package models

// IntermediateDataField wraps IntermediateData for struct literals in tests and code.
func IntermediateDataField(data IntermediateData) jsonIntermediate {
	return jsonIntermediate{value: data}
}

// CriterionField wraps a criterion value for struct literals.
func CriterionField(value any) jsonCriterion {
	return jsonCriterion{value: value}
}

// InvocationEventsField wraps InvocationEvents for struct literals in tests and code.
func InvocationEventsField(events InvocationEvents) jsonIntermediate {
	return jsonIntermediate{value: events}
}
