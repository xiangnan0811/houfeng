//go:build linux && amd64 && !cgo

package main

// canObserveOwnerDeath allows actual observation only while the sole latched
// reason is verified checkpoint-owner death and all independent monitors live.
func (b *authorityBroker) canObserveOwnerDeath() bool {
	if b == nil || b.checkpoint == nil || b.authority == nil || b.reader == nil || b.join == nil {
		return false
	}
	c := b.checkpoint
	if !c.invalid || !c.ownerDead || c.faulted || b.reader.invalid || b.join.invalid != nil {
		return false
	}
	if b.reader.healthy() != nil {
		c.markFault()
		return false
	}
	s, err := b.authority.guard.status()
	if err != nil || s.State != 1 || s.Sticky != 0 || s.Lost != 0 || s.ProbeMissed != 0 || s.BrokerCookie != b.authority.brokerCookie || s.Epoch != b.authority.seal.Epoch || s.LinkIDs != b.authority.seal.LinkIDs {
		c.markFault()
		return false
	}
	return true
}

func (b *authorityBroker) canDrainCommitted(t *runtimeEffectTransaction) bool {
	if b == nil || b.effects == nil || b.checkpoint == nil || t == nil || b.effects.transactions[t.id] != t {
		return false
	}
	if t.state != "committed" && t.state != "effect_observed" && t.state != "response_settled" {
		return false
	}
	return !b.checkpoint.invalid || b.canObserveOwnerDeath()
}
