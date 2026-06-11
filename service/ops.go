package service

func init() {
	Registry = append(Registry, opSet, opList, opUpdate, opOrient, opCreate, opGet, opTopoSort, opLink, opReplace)
}
