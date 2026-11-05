package main

type starredOperation struct {
	IDs   []string
	Value bool
}

func (a *app) enqueueStarred(op starredOperation) {
	if a.store == nil || a.loadError != nil || len(op.IDs) == 0 {
		return
	}
	op.IDs = append([]string(nil), op.IDs...)
	a.enqueueLibraryJob(libraryJob{run: func(store *libraryStore) func() {
		snapshot, err := store.setStarredSnapshot(op.IDs, op.Value)
		return func() {
			if err != nil {
				const title = "Could not update Starred"
				a.report(title, err.Error(), "Open the capture’s context menu and try the Starred action again.")
				a.announce(title)
				return
			}
			a.publishSnapshot(snapshot)
			if op.Value {
				a.announce("Added to Starred")
			} else {
				a.announce("Removed from Starred")
			}
		}
	}})
}
