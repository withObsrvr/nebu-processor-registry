package trade_extractor

import (
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"
)

func alphaNum4(code string, issuer xdr.AccountId) xdr.Asset {
	var raw [4]byte
	copy(raw[:], code)
	return xdr.Asset{
		Type:      xdr.AssetTypeAssetTypeCreditAlphanum4,
		AlphaNum4: &xdr.AlphaNum4{AssetCode: raw, Issuer: issuer},
	}
}

func alphaNum12(code string, issuer xdr.AccountId) xdr.Asset {
	var raw [12]byte
	copy(raw[:], code)
	return xdr.Asset{
		Type:       xdr.AssetTypeAssetTypeCreditAlphanum12,
		AlphaNum12: &xdr.AlphaNum12{AssetCode: raw, Issuer: issuer},
	}
}

func accountID(t *testing.T, address string) xdr.AccountId {
	t.Helper()
	var id xdr.AccountId
	if err := id.SetAddress(address); err != nil {
		t.Fatalf("SetAddress(%q): %v", address, err)
	}
	return id
}

const (
	issuerAddress = "GDKZ2ZX6A5SDIXISLU56SFTZTFGXPD47JRANBQNNXSWYXW5CGOGGVC4V"
	sellerAddress = "GBLVLKGRDU66WLWY4XRORJXCC4LDZ347AQTUYBEPBABIZTVITW2OAGIP"
)

func TestAssetOf(t *testing.T) {
	issuer := accountID(t, issuerAddress)

	tests := []struct {
		name       string
		asset      xdr.Asset
		wantCode   string
		wantIssuer string
	}{
		{
			name:     "native has no issuer",
			asset:    xdr.Asset{Type: xdr.AssetTypeAssetTypeNative},
			wantCode: "XLM",
		},
		{
			// The code is a fixed-width byte array, so a short code arrives
			// padded with NULs. Leaving them in produces an asset code that
			// compares unequal to the same code everywhere else.
			name:       "four-character code is unpadded",
			asset:      alphaNum4("USDC", issuer),
			wantCode:   "USDC",
			wantIssuer: issuerAddress,
		},
		{
			name:       "shorter code keeps no trailing NULs",
			asset:      alphaNum4("EUR", issuer),
			wantCode:   "EUR",
			wantIssuer: issuerAddress,
		},
		{
			name:       "twelve-character code",
			asset:      alphaNum12("LONGASSET", issuer),
			wantCode:   "LONGASSET",
			wantIssuer: issuerAddress,
		},
		{
			// A pool share cannot be sold or bought on an offer, so this arm
			// should not appear; returning an empty asset rather than
			// guessing keeps a wrong code out of the output.
			name:  "pool share yields an empty asset",
			asset: xdr.Asset{Type: xdr.AssetTypeAssetTypePoolShare},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := assetOf(tt.asset)
			if got.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", got.Code, tt.wantCode)
			}
			if got.Issuer != tt.wantIssuer {
				t.Errorf("issuer = %q, want %q", got.Issuer, tt.wantIssuer)
			}
		})
	}
}

func TestClaimToTrade(t *testing.T) {
	seller := accountID(t, sellerAddress)
	issuer := accountID(t, issuerAddress)
	usdc := alphaNum4("USDC", issuer)
	xlm := xdr.Asset{Type: xdr.AssetTypeAssetTypeNative}

	// A close time with a millisecond component, so the two timestamp fields
	// cannot accidentally agree: Protocol 30 records close times in
	// milliseconds and trades in one ledger are ordered within the second.
	const closeTimeMs int64 = 1765158311123

	tests := []struct {
		name     string
		claim    claim
		wantNil  bool
		wantType string
		check    func(t *testing.T, trade *TradeEvent)
	}{
		{
			name: "orderbook claim carries the seller and the offer id",
			claim: claim{
				tradeType: "orderbook",
				claim: xdr.ClaimAtom{
					Type: xdr.ClaimAtomTypeClaimAtomTypeOrderBook,
					OrderBook: &xdr.ClaimOfferAtom{
						SellerId:     seller,
						OfferId:      xdr.Int64(4242),
						AssetSold:    usdc,
						AmountSold:   xdr.Int64(1000),
						AssetBought:  xlm,
						AmountBought: xdr.Int64(2000),
					},
				},
			},
			wantType: "orderbook",
			check: func(t *testing.T, trade *TradeEvent) {
				if trade.Seller != sellerAddress {
					t.Errorf("seller = %q", trade.Seller)
				}
				if trade.OfferID != 4242 {
					t.Errorf("offer id = %d, want 4242", trade.OfferID)
				}
				if trade.SoldAsset.Code != "USDC" || trade.SoldAmount != "1000" {
					t.Errorf("sold = %+v %q", trade.SoldAsset, trade.SoldAmount)
				}
				if trade.BoughtAsset.Code != "XLM" || trade.BoughtAmount != "2000" {
					t.Errorf("bought = %+v %q", trade.BoughtAsset, trade.BoughtAmount)
				}
				if trade.PoolID != "" {
					t.Errorf("orderbook trade has a pool id: %q", trade.PoolID)
				}
			},
		},
		{
			// The V0 arm predates account ids in claim atoms and carries a
			// raw ed25519 key, which still has to render as a G... address.
			name: "v0 claim encodes the raw seller key as an address",
			claim: claim{
				tradeType: "orderbook",
				claim: xdr.ClaimAtom{
					Type: xdr.ClaimAtomTypeClaimAtomTypeV0,
					V0: &xdr.ClaimOfferAtomV0{
						SellerEd25519: *seller.Ed25519,
						OfferId:       xdr.Int64(7),
						AssetSold:     xlm,
						AmountSold:    xdr.Int64(5),
						AssetBought:   usdc,
						AmountBought:  xdr.Int64(6),
					},
				},
			},
			wantType: "orderbook",
			check: func(t *testing.T, trade *TradeEvent) {
				if trade.Seller != sellerAddress {
					t.Errorf("seller = %q, want %q", trade.Seller, sellerAddress)
				}
				if trade.OfferID != 7 {
					t.Errorf("offer id = %d, want 7", trade.OfferID)
				}
			},
		},
		{
			// A pool answered instead of an offer, so the trade type is the
			// pool regardless of which operation reached it, and there is no
			// seller account and no offer id to report.
			name: "liquidity pool claim overrides the trade type",
			claim: claim{
				tradeType: "path_payment",
				claim: xdr.ClaimAtom{
					Type: xdr.ClaimAtomTypeClaimAtomTypeLiquidityPool,
					LiquidityPool: &xdr.ClaimLiquidityAtom{
						LiquidityPoolId: xdr.PoolId{0x01, 0x02},
						AssetSold:       usdc,
						AmountSold:      xdr.Int64(11),
						AssetBought:     xlm,
						AmountBought:    xdr.Int64(12),
					},
				},
			},
			wantType: "liquidity_pool",
			check: func(t *testing.T, trade *TradeEvent) {
				if trade.Seller != "" {
					t.Errorf("pool trade has a seller: %q", trade.Seller)
				}
				if trade.OfferID != 0 {
					t.Errorf("pool trade has an offer id: %d", trade.OfferID)
				}
				if trade.PoolID == "" {
					t.Error("pool trade has no pool id")
				}
			},
		},
		{
			// An arm added by a future protocol. Returning nil lets the
			// caller report it rather than emit a trade with empty assets.
			name: "unknown claim arm yields nothing",
			claim: claim{
				tradeType: "orderbook",
				claim:     xdr.ClaimAtom{Type: xdr.ClaimAtomType(99)},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trade := tt.claim.toTrade(60200000, closeTimeMs, "abc123", 2, sellerAddress, true)

			if tt.wantNil {
				if trade != nil {
					t.Fatalf("got a trade for an unreadable claim: %+v", trade)
				}
				return
			}
			if trade == nil {
				t.Fatal("got no trade")
			}

			if trade.TradeType != tt.wantType {
				t.Errorf("trade type = %q, want %q", trade.TradeType, tt.wantType)
			}
			if trade.LedgerSequence != 60200000 {
				t.Errorf("ledger sequence = %d", trade.LedgerSequence)
			}
			if trade.TimestampUnix != 1765158311 {
				t.Errorf("timestamp_unix = %d, want 1765158311", trade.TimestampUnix)
			}
			if trade.Timestamp != "2025-12-08T01:45:11.123Z" {
				t.Errorf("timestamp = %q, want the millisecond close time", trade.Timestamp)
			}
			if trade.TxHash != "abc123" || trade.OpIndex != 2 || !trade.InSuccessfulTx {
				t.Errorf("transaction context = %q %d %v", trade.TxHash, trade.OpIndex, trade.InSuccessfulTx)
			}
			tt.check(t, trade)
		})
	}
}

func TestClaimsOf(t *testing.T) {
	seller := accountID(t, sellerAddress)
	atom := xdr.ClaimAtom{
		Type:      xdr.ClaimAtomTypeClaimAtomTypeOrderBook,
		OrderBook: &xdr.ClaimOfferAtom{SellerId: seller},
	}

	sellOffer := func(claimed []xdr.ClaimAtom, code xdr.ManageSellOfferResultCode) xdr.OperationResult {
		return xdr.OperationResult{
			Code: xdr.OperationResultCodeOpInner,
			Tr: &xdr.OperationResultTr{
				Type: xdr.OperationTypeManageSellOffer,
				ManageSellOfferResult: &xdr.ManageSellOfferResult{
					Code: code,
					Success: &xdr.ManageOfferSuccessResult{
						OffersClaimed: claimed,
					},
				},
			},
		}
	}

	tests := []struct {
		name      string
		result    xdr.OperationResult
		wantCount int
		wantType  string
	}{
		{
			name:      "one operation can claim several offers",
			result:    sellOffer([]xdr.ClaimAtom{atom, atom, atom}, xdr.ManageSellOfferResultCodeManageSellOfferSuccess),
			wantCount: 3,
			wantType:  "orderbook",
		},
		{
			name:      "an offer that matched nothing produces no trades",
			result:    sellOffer(nil, xdr.ManageSellOfferResultCodeManageSellOfferSuccess),
			wantCount: 0,
		},
		{
			// A failed operation has no Success arm to read.
			name:      "a failed offer produces no trades",
			result:    sellOffer([]xdr.ClaimAtom{atom}, xdr.ManageSellOfferResultCodeManageSellOfferUnderfunded),
			wantCount: 0,
		},
		{
			// The transaction failed before the operation ran, so there is no
			// inner result at all.
			name:      "a result with no inner result produces no trades",
			result:    xdr.OperationResult{Code: xdr.OperationResultCodeOpNoAccount},
			wantCount: 0,
		},
		{
			name: "a path payment's offers are path_payment trades",
			result: xdr.OperationResult{
				Code: xdr.OperationResultCodeOpInner,
				Tr: &xdr.OperationResultTr{
					Type: xdr.OperationTypePathPaymentStrictSend,
					PathPaymentStrictSendResult: &xdr.PathPaymentStrictSendResult{
						Code: xdr.PathPaymentStrictSendResultCodePathPaymentStrictSendSuccess,
						Success: &xdr.PathPaymentStrictSendResultSuccess{
							Offers: []xdr.ClaimAtom{atom, atom},
						},
					},
				},
			},
			wantCount: 2,
			wantType:  "path_payment",
		},
		{
			// Not an operation that can trade.
			name: "an unrelated operation produces no trades",
			result: xdr.OperationResult{
				Code: xdr.OperationResultCodeOpInner,
				Tr: &xdr.OperationResultTr{
					Type:          xdr.OperationTypePayment,
					PaymentResult: &xdr.PaymentResult{Code: xdr.PaymentResultCodePaymentSuccess},
				},
			},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := claimsOf(tt.result)
			if len(claims) != tt.wantCount {
				t.Fatalf("claims = %d, want %d", len(claims), tt.wantCount)
			}
			for _, c := range claims {
				if c.tradeType != tt.wantType {
					t.Errorf("trade type = %q, want %q", c.tradeType, tt.wantType)
				}
			}
		})
	}
}
