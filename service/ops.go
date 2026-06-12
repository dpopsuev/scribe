package service

func init() {
	Registry = append(Registry, opSet, opQuery, opUpdate, opOrient, opCreate, opGet, opLink, opReplace)
}
