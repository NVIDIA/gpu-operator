package certification

var testSetRegistry = make(map[string]*TestSet)

func RegisterTestSet(ts *TestSet) {
	testSetRegistry[ts.Name] = ts
}

func AllTestSets() map[string]*TestSet {
	return testSetRegistry
}

func TestSetByName(name string) *TestSet {
	return testSetRegistry[name]
}
