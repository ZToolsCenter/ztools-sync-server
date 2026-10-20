package sync

import "github.com/ZToolsCenter/ztools-sync-server/repository"

// revisionHistoryReader keeps only parent links, scoped to one user's pull request.
// It never retains document bodies or shares cached history across requests.
type revisionHistoryReader struct {
	repo         *repository.Repository
	uid          string
	parentsByDoc map[string]map[string]string
}

func newRevisionHistoryReader(repo *repository.Repository, uid string) *revisionHistoryReader {
	return &revisionHistoryReader{
		repo:         repo,
		uid:          uid,
		parentsByDoc: make(map[string]map[string]string),
	}
}

// History loads each document's links once and follows the requested branch.
func (r *revisionHistoryReader) History(docID string, rev string) ([]string, error) {
	parents, ok := r.parentsByDoc[docID]
	if !ok {
		var err error
		parents, err = r.repo.DocumentRevisionParents(r.uid, docID)
		if err != nil {
			return nil, err
		}
		r.parentsByDoc[docID] = parents
	}
	return revisionHistoryFromParents(rev, parents), nil
}

func revisionHistoryFromParents(rev string, parents map[string]string) []string {
	history := []string{}
	seen := map[string]bool{}
	for rev != "" && !seen[rev] {
		history = append(history, rev)
		seen[rev] = true
		rev = parents[rev]
	}
	return history
}
