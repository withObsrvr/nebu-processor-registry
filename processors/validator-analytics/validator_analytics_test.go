package validator_analytics

import (
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"
	"github.com/withObsrvr/nebu/pkg/processor"
)

func TestOriginContract(t *testing.T) {
	origin := NewOrigin("test network")
	defer origin.Close()

	if got, want := origin.Name(), processorName; got != want {
		t.Fatalf("Name() = %q, want %q", got, want)
	}
	if got, want := origin.Type(), processor.TypeOrigin; got != want {
		t.Fatalf("Type() = %v, want %v", got, want)
	}
}

func TestClassifyOperation(t *testing.T) {
	tests := []struct {
		name          string
		operationType xdr.OperationType
		want          operationCategory
	}{
		{"create account", xdr.OperationTypeCreateAccount, categoryAccountCreation},
		{"payment", xdr.OperationTypePayment, categoryPayments},
		{"strict receive", xdr.OperationTypePathPaymentStrictReceive, categoryPayments},
		{"strict send", xdr.OperationTypePathPaymentStrictSend, categoryPayments},
		{"sell offer", xdr.OperationTypeManageSellOffer, categoryOffersAndAMMs},
		{"passive offer", xdr.OperationTypeCreatePassiveSellOffer, categoryOffersAndAMMs},
		{"buy offer", xdr.OperationTypeManageBuyOffer, categoryOffersAndAMMs},
		{"pool deposit", xdr.OperationTypeLiquidityPoolDeposit, categoryOffersAndAMMs},
		{"pool withdraw", xdr.OperationTypeLiquidityPoolWithdraw, categoryOffersAndAMMs},
		{"change trust", xdr.OperationTypeChangeTrust, categoryTrustlines},
		{"allow trust", xdr.OperationTypeAllowTrust, categoryTrustlines},
		{"trustline flags", xdr.OperationTypeSetTrustLineFlags, categoryTrustlines},
		{"create claimable balance", xdr.OperationTypeCreateClaimableBalance, categoryClaimableBalances},
		{"claim claimable balance", xdr.OperationTypeClaimClaimableBalance, categoryClaimableBalances},
		{"clawback claimable balance", xdr.OperationTypeClawbackClaimableBalance, categoryClaimableBalances},
		{"begin sponsorship", xdr.OperationTypeBeginSponsoringFutureReserves, categorySponsorship},
		{"end sponsorship", xdr.OperationTypeEndSponsoringFutureReserves, categorySponsorship},
		{"revoke sponsorship", xdr.OperationTypeRevokeSponsorship, categorySponsorship},
		{"invoke host function", xdr.OperationTypeInvokeHostFunction, categorySoroban},
		{"extend footprint ttl", xdr.OperationTypeExtendFootprintTtl, categorySoroban},
		{"restore footprint", xdr.OperationTypeRestoreFootprint, categorySoroban},
		{"inflation is not account creation", xdr.OperationTypeInflation, categoryOther},
		{"future operation", xdr.OperationType(999), categoryOther},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyOperation(test.operationType); got != test.want {
				t.Fatalf("classifyOperation(%v) = %v, want %v", test.operationType, got, test.want)
			}
		})
	}
}

func TestSummarizeActivitySeparatesFailedTransactions(t *testing.T) {
	summary := summarizeActivity([]transactionFacts{
		{successful: true, operationTypes: []xdr.OperationType{
			xdr.OperationTypePayment,
			xdr.OperationTypeInvokeHostFunction,
		}},
		{successful: false, operationTypes: []xdr.OperationType{
			xdr.OperationTypeCreateAccount,
			xdr.OperationTypeInflation,
		}},
	})

	if got, want := summary.transactionCount, uint32(2); got != want {
		t.Fatalf("transaction count = %d, want %d", got, want)
	}
	if got, want := summary.successfulTransactionCount, uint32(1); got != want {
		t.Fatalf("successful transaction count = %d, want %d", got, want)
	}
	if got, want := summary.operationCount, uint32(4); got != want {
		t.Fatalf("operation count = %d, want %d", got, want)
	}
	if got, want := summary.successfulOperationCount, uint32(2); got != want {
		t.Fatalf("successful operation count = %d, want %d", got, want)
	}

	all := summary.operationCategories
	if all.Payments != 1 || all.Soroban != 1 || all.AccountCreation != 1 || all.Other != 1 {
		t.Fatalf("unexpected all-operation categories: %+v", all)
	}
	successful := summary.successfulCategories
	if successful.Payments != 1 || successful.Soroban != 1 {
		t.Fatalf("unexpected successful-operation categories: %+v", successful)
	}
	if successful.AccountCreation != 0 || successful.Other != 0 {
		t.Fatalf("failed transaction operations leaked into successful categories: %+v", successful)
	}
}
