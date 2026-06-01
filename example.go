package main

type N23 struct{}

// talia:fun(insert)
func insert(n *N23, k rune) bool {
	// talia:pre(has parent) → panic
	if hasParent(n) {
		panic("insert into non root node")
	}
	var target *N23
	// talia:loop(find target)
	for {
		target = getChild(target, k)
		// talia:cond(target has key)
		if hasKey(target, k) {
			// talia:return(found)
			return true
		} // talia:done
		if isLeaf(target) {
			// talia:break(is leaf)
			break
		}
	} // talia:done

	addKey(target, k)

	// talia:loop(merge up)
	for {
		if !keyCountEQ(target, 3) {
			// talia:break(no overflow)
			break
		}

		split := split(target)
		parent := parent(target)
		remove(target)
		target = merge(parent, split)
	} // talia:done
	return false // talia:return(not found)
}

func merge(parent *N23, split *N23) *N23 {
	panic("unimplemented")
}

func remove(target *N23) {
	panic("unimplemented")
}

func parent(target *N23) *N23 {
	panic("unimplemented")
}

func split(target *N23) *N23 {
	panic("unimplemented")
}

func keyCountEQ(target *N23, i int) bool {
	panic("unimplemented")
}

func addKey(target *N23, k rune) {
	panic("unimplemented")
}

func isLeaf(target *N23) bool {
	panic("unimplemented")
}

func hasKey(target *N23, k rune) bool {
	panic("unimplemented")
}

func getChild(target *N23, k rune) *N23 {
	panic("unimplemented")
}

func hasParent(n *N23) bool {
	panic("unimplemented")
}
