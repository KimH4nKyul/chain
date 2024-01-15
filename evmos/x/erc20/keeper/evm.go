// Copyright 2022 Evmos Foundation
// This file is part of the Evmos Network packages.
//
// Evmos is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The Evmos packages are distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the Evmos packages. If not, see https://github.com/evmos/evmos/blob/main/LICENSE

package keeper

import (
	"math/big"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	evmtypes "github.com/settlus/chain/evmos/x/evm/types"

	"github.com/settlus/chain/contracts"
	"github.com/settlus/chain/evmos/x/erc20/types"
)

// DeployERC20Contract creates and deploys an ERC20 contract on the EVM with the
// erc20 module account as owner.
func (k Keeper) DeployERC20Contract(
	ctx sdk.Context,
	coinMetadata banktypes.Metadata,
) (common.Address, error) {
	decimals := uint8(0)
	if len(coinMetadata.DenomUnits) > 0 {
		decimalsIdx := len(coinMetadata.DenomUnits) - 1
		decimals = uint8(coinMetadata.DenomUnits[decimalsIdx].Exponent)
	}
	ctorArgs, err := contracts.ERC20Contract.ABI.Pack(
		"",
		coinMetadata.Name,
		coinMetadata.Symbol,
		decimals,
	)
	if err != nil {
		return common.Address{}, errorsmod.Wrapf(types.ErrABIPack, "coin metadata is invalid %s: %s", coinMetadata.Name, err.Error())
	}

	data := make([]byte, len(contracts.ERC20Contract.Bin)+len(ctorArgs))
	copy(data[:len(contracts.ERC20Contract.Bin)], contracts.ERC20Contract.Bin)
	copy(data[len(contracts.ERC20Contract.Bin):], ctorArgs)

	nonce, err := k.accountKeeper.GetSequence(ctx, types.ModuleAddress.Bytes())
	if err != nil {
		return common.Address{}, err
	}

	contractAddr := crypto.CreateAddress(types.ModuleAddress, nonce)
	_, err = k.evmKeeper.CallEVMWithData(ctx, types.ModuleAddress, nil, data, true)
	if err != nil {
		return common.Address{}, errorsmod.Wrapf(err, "failed to deploy contract for %s", coinMetadata.Name)
	}

	return contractAddr, nil
}

// QueryERC20 returns the data of a deployed ERC20 contract
func (k Keeper) QueryERC20(
	ctx sdk.Context,
	contract common.Address,
) (types.ERC20Data, error) {
	var (
		nameRes    types.ERC20StringResponse
		symbolRes  types.ERC20StringResponse
		decimalRes types.ERC20Uint8Response
	)

	erc20 := contracts.ERC20Contract.ABI

	// Name
	res, err := k.evmKeeper.CallEVM(ctx, erc20, types.ModuleAddress, contract, false, "name")
	if err != nil {
		return types.ERC20Data{}, err
	}

	if err := erc20.UnpackIntoInterface(&nameRes, "name", res.Ret); err != nil {
		return types.ERC20Data{}, errorsmod.Wrapf(
			types.ErrABIUnpack, "failed to unpack name: %s", err.Error(),
		)
	}

	// Symbol
	res, err = k.evmKeeper.CallEVM(ctx, erc20, types.ModuleAddress, contract, false, "symbol")
	if err != nil {
		return types.ERC20Data{}, err
	}

	if err := erc20.UnpackIntoInterface(&symbolRes, "symbol", res.Ret); err != nil {
		return types.ERC20Data{}, errorsmod.Wrapf(
			types.ErrABIUnpack, "failed to unpack symbol: %s", err.Error(),
		)
	}

	// Decimals
	res, err = k.evmKeeper.CallEVM(ctx, erc20, types.ModuleAddress, contract, false, "decimals")
	if err != nil {
		return types.ERC20Data{}, err
	}

	if err := erc20.UnpackIntoInterface(&decimalRes, "decimals", res.Ret); err != nil {
		return types.ERC20Data{}, errorsmod.Wrapf(
			types.ErrABIUnpack, "failed to unpack decimals: %s", err.Error(),
		)
	}

	return types.NewERC20Data(nameRes.Value, symbolRes.Value, decimalRes.Value), nil
}

// BalanceOf queries an account's balance for a given ERC20 contract
func (k Keeper) BalanceOf(
	ctx sdk.Context,
	abi abi.ABI,
	contract, account common.Address,
) *big.Int {
	res, err := k.evmKeeper.CallEVM(ctx, abi, types.ModuleAddress, contract, false, "balanceOf", account)
	if err != nil {
		return nil
	}

	unpacked, err := abi.Unpack("balanceOf", res.Ret)
	if err != nil || len(unpacked) == 0 {
		return nil
	}

	balance, ok := unpacked[0].(*big.Int)
	if !ok {
		return nil
	}

	return balance
}

// monitorApprovalEvent returns an error if the given transactions logs include
// an unexpected `Approval` event
func (k Keeper) monitorApprovalEvent(res *evmtypes.MsgEthereumTxResponse) error {
	if res == nil || len(res.Logs) == 0 {
		return nil
	}

	logApprovalSig := []byte("Approval(address,address,uint256)")
	logApprovalSigHash := crypto.Keccak256Hash(logApprovalSig)

	for _, log := range res.Logs {
		if log.Topics[0] == logApprovalSigHash.Hex() {
			return errorsmod.Wrapf(
				types.ErrUnexpectedEvent, "unexpected Approval event",
			)
		}
	}

	return nil
}
