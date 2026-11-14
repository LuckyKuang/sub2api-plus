//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserVisibleStandardGroup(t *testing.T) {
	active := &Group{ID: 7, SubscriptionType: SubscriptionTypeStandard, Status: StatusActive}
	exclusive := &Group{ID: 8, SubscriptionType: SubscriptionTypeStandard, Status: StatusActive, IsExclusive: true}
	disabled := &Group{ID: 9, SubscriptionType: SubscriptionTypeStandard, Status: "inactive"}
	subscription := &Group{ID: 10, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive}

	user := &User{AllowedGroups: []int64{7}}
	require.True(t, UserVisibleStandardGroup(user, active), "granted public group")
	require.False(t, UserVisibleStandardGroup(user, exclusive), "exclusive group needs a grant")

	restricted := &User{RestrictPublicGroups: true}
	require.False(t, UserVisibleStandardGroup(restricted, active), "restricted user without a grant")
	require.False(t, UserVisibleStandardGroup(&User{AllowedGroups: []int64{7}, RestrictPublicGroups: true}, disabled), "disabled group")
	require.False(t, UserVisibleStandardGroup(user, subscription), "subscription groups follow their own rule")
}

func TestHideUnavailableStandardGroups(t *testing.T) {
	user := &User{ID: 1, AllowedGroups: []int64{7}, RestrictPublicGroups: true}
	svc := &APIKeyService{
		userRepo: &visibilityUserRepo{user: user},
		groupRepo: &visibilityGroupRepo{groups: []Group{
			{ID: 7, SubscriptionType: SubscriptionTypeStandard, Status: StatusActive},
			{ID: 8, SubscriptionType: SubscriptionTypeStandard, Status: StatusActive, IsExclusive: true},
			{ID: 9, SubscriptionType: SubscriptionTypeStandard, Status: "inactive"},
		}},
	}
	groupSeven, groupEight, groupNine, groupTen := int64(7), int64(8), int64(9), int64(10)
	keys := []APIKey{
		{ID: 1, GroupID: &groupSeven, Group: &Group{ID: 7, SubscriptionType: SubscriptionTypeStandard, Status: StatusActive}},
		{ID: 2, GroupID: &groupEight, Group: &Group{ID: 8, SubscriptionType: SubscriptionTypeStandard, Status: StatusActive, IsExclusive: true}},
		{ID: 3, GroupID: &groupNine, Group: &Group{ID: 9, SubscriptionType: SubscriptionTypeStandard, Status: "inactive"}},
		{ID: 4, GroupID: &groupTen, Group: &Group{ID: 10, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive}},
		{ID: 5, GroupBindingMode: GroupBindingCustom, AggregateGroupIDs: []int64{7, 8, 9}},
	}

	svc.hideUnavailableStandardGroups(context.Background(), 1, keys)

	require.NotNil(t, keys[0].Group, "granted standard group stays visible")
	require.Nil(t, keys[1].Group, "exclusive group without a grant is hidden")
	require.Nil(t, keys[2].Group, "disabled group is hidden")
	require.NotNil(t, keys[3].Group, "subscription groups keep their own rule")
	require.Equal(t, []int64{7}, keys[4].AggregateGroupIDs, "aggregate scope keeps usable groups only")
}
