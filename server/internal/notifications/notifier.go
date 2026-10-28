package notifications

import "context"

// Notifier files notifications: each lands in the inbox, and is queued for
// push when the user's preference for its league and type is on.
type Notifier struct {
	Store Store
}

// NewNotifier returns a Notifier on s.
func NewNotifier(s Store) *Notifier { return &Notifier{Store: s} }

// Send files ns, skipping any whose key the user already has. It returns
// the notifications that were new.
func (n *Notifier) Send(ctx context.Context, ns ...Notification) ([]Notification, error) {
	off := map[[2]string]map[Type]bool{}
	var out []Notification
	for _, x := range ns {
		push := true
		if x.LeagueID != "" {
			k := [2]string{x.UserID, x.LeagueID}
			if off[k] == nil {
				d, err := n.Store.Disabled(ctx, x.UserID, x.LeagueID)
				if err != nil {
					return out, err
				}
				off[k] = d
			}
			push = !off[k][x.Type]
		}
		added, ok, err := n.Store.Add(ctx, x, push)
		if err != nil {
			return out, err
		}
		if ok {
			out = append(out, added)
		}
	}
	return out, nil
}
