package service

func init() {
	Registry = append(Registry, opSet, opQuery, opUpdate, opCreate, opGet, opLink, opLint, opSynthesize)
}
