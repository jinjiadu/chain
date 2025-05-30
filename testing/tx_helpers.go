package testing

// Copy from https://github.com/cosmos/cosmos-sdk/blob/v0.50.9/testutil/sims/tx_helpers.go to add time in block header and block events
import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	types2 "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsign "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

// GenSignedMockTx generates a signed mock transaction.
func GenSignedMockTx(
	r *rand.Rand,
	txConfig client.TxConfig,
	msgs []sdk.Msg,
	feeAmt sdk.Coins,
	gas uint64,
	chainID string,
	accNums, accSeqs []uint64,
	priv ...cryptotypes.PrivKey,
) (sdk.Tx, error) {
	sigs := make([]signing.SignatureV2, len(priv))

	// create a random length memo
	memo := simulation.RandStringOfLength(r, simulation.RandIntBetween(r, 0, 100))

	signMode, err := authsign.APISignModeToInternal(txConfig.SignModeHandler().DefaultMode())
	if err != nil {
		return nil, err
	}

	// 1st round: set SignatureV2 with empty signatures, to set correct
	// signer infos.
	for i, p := range priv {
		sigs[i] = signing.SignatureV2{
			PubKey: p.PubKey(),
			Data: &signing.SingleSignatureData{
				SignMode: signMode,
			},
			Sequence: accSeqs[i],
		}
	}

	tx := txConfig.NewTxBuilder()
	err = tx.SetMsgs(msgs...)
	if err != nil {
		return nil, err
	}
	err = tx.SetSignatures(sigs...)
	if err != nil {
		return nil, err
	}
	tx.SetMemo(memo)
	tx.SetFeeAmount(feeAmt)
	tx.SetGasLimit(gas)

	// 2nd round: once all signer infos are set, every signer can sign.
	for i, p := range priv {
		signerData := authsign.SignerData{
			Address:       sdk.AccAddress(p.PubKey().Address()).String(),
			ChainID:       chainID,
			AccountNumber: accNums[i],
			Sequence:      accSeqs[i],
			PubKey:        p.PubKey(),
		}

		signBytes, err := authsign.GetSignBytesAdapter(
			context.Background(), txConfig.SignModeHandler(), signMode, signerData,
			tx.GetTx())
		if err != nil {
			panic(err)
		}
		sig, err := p.Sign(signBytes)
		if err != nil {
			panic(err)
		}
		sigs[i].Data.(*signing.SingleSignatureData).Signature = sig
	}
	err = tx.SetSignatures(sigs...)
	if err != nil {
		panic(err)
	}

	return tx.GetTx(), nil
}

// SignCheckDeliver checks a generated signed transaction and simulates a
// block commitment with the given transaction. A test assertion is made using
// the parameter 'expPass' against the result. A corresponding result is
// returned.
func SignCheckDeliver(
	t *testing.T, txCfg client.TxConfig, app *baseapp.BaseApp, header types.Header, msgs []sdk.Msg,
	chainID string, accNums, accSeqs []uint64, expSimPass, expPass bool, priv ...cryptotypes.PrivKey,
) (sdk.GasInfo, *sdk.Result, []types2.Event, error) {
	tx, err := GenSignedMockTx(
		rand.New(rand.NewSource(time.Now().UnixNano())),
		txCfg,
		msgs,
		sdk.Coins{sdk.NewInt64Coin(sdk.DefaultBondDenom, 0)},
		DefaultGenTxGas,
		chainID,
		accNums,
		accSeqs,
		priv...,
	)
	require.NoError(t, err)
	txBytes, err := txCfg.TxEncoder()(tx)
	require.Nil(t, err)

	// Must simulate now as CheckTx doesn't run Msgs anymore
	_, res, err := app.Simulate(txBytes)

	if expSimPass {
		require.NoError(t, err)
		require.NotNil(t, res)
	} else {
		require.Error(t, err)
		require.Nil(t, res)
	}

	bz, err := txCfg.TxEncoder()(tx)
	require.NoError(t, err)

	resBlock, err := app.FinalizeBlock(&types2.RequestFinalizeBlock{
		Height: header.Height,
		Time:   header.Time,
		Txs:    [][]byte{bz},
	})
	require.NoError(t, err)

	require.Equal(t, 1, len(resBlock.TxResults))
	txResult := resBlock.TxResults[0]
	finalizeSuccess := txResult.Code == 0
	if expPass {
		require.True(t, finalizeSuccess)
	} else {
		require.False(t, finalizeSuccess)
	}

	_, _ = app.Commit()

	gInfo := sdk.GasInfo{GasWanted: uint64(txResult.GasWanted), GasUsed: uint64(txResult.GasUsed)}
	txRes := sdk.Result{Data: txResult.Data, Log: txResult.Log, Events: txResult.Events}
	if finalizeSuccess {
		err = nil
	} else {
		err = errors.ABCIError(txResult.Codespace, txResult.Code, txResult.Log)
	}

	endblockEvent := resBlock.Events

	return gInfo, &txRes, endblockEvent, err
}

func GenSequenceOfTxs(
	txEncoder sdk.TxEncoder,
	txConfig client.TxConfig,
	msgs []sdk.Msg,
	account *AccountWithNumSeq,
	feeAmount sdk.Coins,
	gas uint64,
	numTxs int,
) [][]byte {
	txs := make([][]byte, numTxs)

	for i := 0; i < numTxs; i++ {
		tx, _ := GenSignedMockTx(
			rand.New(rand.NewSource(time.Now().UnixNano())),
			txConfig,
			msgs,
			feeAmount,
			gas,
			ChainID,
			[]uint64{account.Num},
			[]uint64{account.Seq},
			account.PrivKey,
		)

		txs[i], _ = txEncoder(tx)
		account.Seq++
	}

	return txs
}
