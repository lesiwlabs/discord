package main

type set[E comparable] map[E]struct{}

func newSet[E comparable](elements ...E) set[E] {
	s := make(set[E])
	for _, e := range elements {
		s[e] = struct{}{}
	}
	return s
}

func (s set[E]) Add(e E) {
	s[e] = struct{}{}
}

func (s set[E]) Union(o set[E]) {
	for e := range o {
		s[e] = struct{}{}
	}
}

func (s set[E]) Diff(o set[E]) set[E] {
	r := newSet[E]()
	for e := range s {
		if _, ok := o[e]; !ok {
			r[e] = struct{}{}
		}
	}
	return r
}
