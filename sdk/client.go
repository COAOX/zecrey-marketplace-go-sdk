package sdk

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr/mimc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/zecrey-labs/zecrey-crypto/util/ecdsaHelper"
	"github.com/zecrey-labs/zecrey-crypto/util/eddsaHelper"
	"github.com/zecrey-labs/zecrey-eth-rpc/_rpc"
	zecreyLegendRpc "github.com/zecrey-labs/zecrey-eth-rpc/zecrey/core/zecrey-legend"
	zecreyLegendUtil "github.com/zecrey-labs/zecrey-legend/common/util"

	"github.com/zeromicro/go-zero/core/logx"

	"github.com/Zecrey-Labs/zecrey-marketplace-go-sdk/sdk/model"
)

var (
	ZecreyLegendContract = "0x5761494e2C0B890dE64aa009AFE9596A5Fbf47A7"
	ZnsPriceOracle       = "0x736922e13c7df2D99D9A244f86815b663DcAAE03"
)

const (
	nftMarketUrl = "http://34.111.87.92/"
	legendUrl    = "https://dev-legend-app.zecrey.com"
	//nftMarketUrl = "http://localhost:9999"
	//nftMarketUrl = "https://test-legend-nft.zecrey.com"
	//legendUrl    = "https://test-legend-app.zecrey.com"
	chainRpcUrl = "https://data-seed-prebsc-1-s1.binance.org:8545"

	DefaultGasLimit = 5000000
	NameSuffix      = ".zec"
)

type client struct {
	nftMarketURL   string
	legendURL      string
	providerClient *_rpc.ProviderClient
	keyManager     KeyManager
}

func (c *client) SetKeyManager(keyManager KeyManager) {
	c.keyManager = keyManager
}

func (c *client) CreateL1Account() (l1Addr, privateKeyStr, l2pk, seed string, err error) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		logx.Errorf("[CreateL1Account] GenerateKey err: %s", err)
		return "", "", "", "", err
	}
	privateKeyStr = hex.EncodeToString(crypto.FromECDSA(privateKey))
	l1Addr, err = ecdsaHelper.GenerateL1Address(privateKey)
	if err != nil {
		logx.Errorf("[CreateL1Account] GenerateL1Address err: %s", err)
		return "", "", "", "", err
	}
	seed, err = eddsaHelper.GetEddsaSeed(privateKey)
	if err != nil {
		logx.Errorf("[CreateL1Account] GetEddsaSeed err: %s", err)
		return "", "", "", "", err
	}
	l2pk = eddsaHelper.GetEddsaPublicKey(seed[2:])
	return
}

func (c *client) RegisterAccountWithPrivateKey(accountName, l1Addr, l2pk, privateKey, seed string) (ZecreyNftMarketSDK, error) {
	if ok, err := c.GetAccountIsRegistered(accountName); ok {
		if err != nil {
			return nil, err
		}
		keyManager, err := NewSeedKeyManager(seed)
		if err != nil {
			return nil, err
		}
		return NewZecreyNftMarketSDK(keyManager), nil
	}
	var chainId *big.Int
	chainId, err := c.providerClient.ChainID(context.Background())
	if err != nil {
		return nil, err
	}
	authCli, err := _rpc.NewAuthClient(c.providerClient, privateKey, chainId)
	if err != nil {
		return nil, err
	}
	px, py, err := zecreyLegendUtil.PubKeyStrToPxAndPy(l2pk)
	if err != nil {
		return nil, err
	}
	//get base contract address
	resp, err := c.GetLayer2BasicInfo()
	if err != nil {
		return nil, err
	}
	ZecreyLegendContract = resp.ContractAddresses[0]
	ZnsPriceOracle = resp.ContractAddresses[1]

	gasPrice, err := c.providerClient.SuggestGasPrice(context.Background())
	if err != nil {
		return nil, err
	}
	zecreyInstance, err := zecreyLegendRpc.LoadZecreyLegendInstance(c.providerClient, ZecreyLegendContract)
	if err != nil {
		return nil, err
	}
	priceOracleInstance, err := zecreyLegendRpc.LoadStablePriceOracleInstance(c.providerClient, ZnsPriceOracle)
	if err != nil {
		return nil, err
	}
	_, err = zecreyLegendRpc.RegisterZNS(c.providerClient, authCli,
		zecreyInstance, priceOracleInstance,
		gasPrice, DefaultGasLimit, accountName,
		common.HexToAddress(l1Addr), px, py)
	if err != nil {
		return nil, err
	}
	keyManager, err := NewSeedKeyManager(seed)
	if err != nil {
		return nil, err
	}

	return NewZecreyNftMarketSDK(keyManager), nil
}

func (c *client) GetAccountIsRegistered(accountName string) (bool, error) {
	res, err := zecreyLegendUtil.ComputeAccountNameHashInBytes(accountName + NameSuffix)
	if err != nil {
		logx.Error(err)
		return false, err
	}
	//get base contract address
	resp, err := c.GetLayer2BasicInfo()
	if err != nil {
		return false, err
	}
	ZecreyLegendContract = resp.ContractAddresses[0]
	ZnsPriceOracle = resp.ContractAddresses[1]

	resBytes := zecreyLegendUtil.SetFixed32Bytes(res)
	zecreyInstance, err := zecreyLegendRpc.LoadZecreyLegendInstance(c.providerClient, ZecreyLegendContract)
	if err != nil {
		return false, err
	}
	// fetch by accountNameHash
	addr, err := zecreyInstance.GetAddressByAccountNameHash(zecreyLegendRpc.EmptyCallOpts(), resBytes)
	if err != nil {
		logx.Error(err)
		return false, err
	}
	return bytes.Equal(addr.Bytes(), BytesToAddress([]byte{}).Bytes()), nil
}

func BytesToAddress(b []byte) common.Address {
	var a common.Address
	a.SetBytes(b)
	return a
}
func (c *client) GetAccountByAccountName(accountName string) (address string, err error) {
	//res, err := zecreyLegendUtil.ComputeAccountNameHashInBytes(accountName + NameSuffix)
	//if err != nil {
	//	logx.Error(err)
	//	return "", err
	//}
	////get base contract address
	//resp, err := c.GetLayer2BasicInfo()
	//if err != nil {
	//	return "", err
	//}
	//ZecreyLegendContract = resp.ContractAddresses[0]
	//ZnsPriceOracle = resp.ContractAddresses[1]
	//
	//resBytes := zecreyLegendUtil.SetFixed32Bytes(res)
	//zecreyInstance, err := zecreyLegendRpc.LoadZecreyLegendInstance(c.providerClient, ZecreyLegendContract)
	//if err != nil {
	//	return "", err
	//}
	//// fetch by accountNameHash
	//addr, err := zecreyInstance.GetAddressByAccountNameHash(zecreyLegendRpc.EmptyCallOpts(), resBytes)
	//if err != nil {
	//	logx.Error(err)
	//	return "", err
	//}
	//return addr.String(), nil

	return "", err
}

func (c *client) ApplyRegisterHost(
	accountName string, l2Pk string, OwnerAddr string) (*RespApplyRegisterHost, error) {
	resp, err := http.PostForm(c.legendURL+"/api/v1/register/applyRegisterHost",
		url.Values{
			"account_name": {accountName},
			"l2_pk":        {l2Pk},
			"owner_addr":   {OwnerAddr}})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(string(body))
	}
	result := &RespApplyRegisterHost{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *client) CreateCollection(
	accountName string, ShortName string, CategoryId string, CreatorEarningRate string,
	ops ...model.CollectionOption) (*RespCreateCollection, error) {
	cp := &model.CollectionParams{}
	for _, do := range ops {
		do.F(cp)
	}

	respPrepareTx, err := http.Get(c.nftMarketURL + fmt.Sprintf("/api/v1/preparetx/getPrepareCreateCollectionTxInfo?account_name=%s", accountName))
	if err != nil {
		return nil, err
	}
	body, err := ioutil.ReadAll(respPrepareTx.Body)
	if err != nil {
		return nil, err
	}
	if respPrepareTx.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(string(body))
	}

	resultPrepare := &RespetPreparetxInfo{}
	if err := json.Unmarshal(body, &resultPrepare); err != nil {
		return nil, err
	}
	tx, err := PrepareCreateCollectionTxInfo(c.keyManager, resultPrepare.Transtion, cp.Description)
	if err != nil {
		return nil, err
	}
	resp, err := http.PostForm(c.nftMarketURL+"/api/v1/collection/createCollection",
		url.Values{"short_name": {ShortName},
			"category_id":          {CategoryId},
			"collection_url":       {cp.CollectionUrl},
			"external_link":        {cp.ExternalLink},
			"twitter_link":         {cp.TwitterLink},
			"instagram_link":       {cp.TelegramLink},
			"discord_link":         {cp.InstagramLink},
			"telegram_link":        {cp.DiscordLink},
			"logo_image":           {cp.LogoImage},
			"featured_image":       {cp.FeaturedImage},
			"banner_image":         {cp.BannerImage},
			"creator_earning_rate": {CreatorEarningRate},
			"payment_asset_ids":    {cp.PaymentAssetIds},
			"transaction":          {tx}})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(string(body))
	}
	result := &RespCreateCollection{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *client) GetCollectionById(collectionId int64) (*RespGetCollectionByCollectionId, error) {
	request_query := fmt.Sprintf("query MyQuery {\n  actionGetCollectionById(collection_id: %d) {\n    collection {\n      account_name\n      banner_thumb\n    }\n  }\n}\n", collectionId)
	input := InputCollectionByIdActionBody{CollectionId: collectionId}
	action := ActionBody{Name: "actionGetCollectionById"}
	SessionVariables := SessionVariablesBody{XHasuraUserId: "x-hasura-role", XHasuraRole: "admin"}
	req := ReqGetCollectionById{
		Input:            input,
		Action:           action,
		SessionVariables: SessionVariables,
		RequestQuery:     request_query,
	}
	statusJSON, _ := json.Marshal(req)
	resp, err := http.Post(c.nftMarketURL+"/api/v1/action/actionGetCollectionById", "application/json", bytes.NewReader(statusJSON))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(string(body))
	}
	result := &RespGetCollectionByCollectionId{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *client) UpdateCollection(Id string, AccountName string, Name string,
	ops ...model.CollectionOption) (*RespUpdateCollection, error) {
	cp := &model.CollectionParams{}
	for _, do := range ops {
		do.F(cp)
	}
	CategoryId := "1"
	timestamp := time.Now().Unix()
	message := fmt.Sprintf("%dupdate_collection", timestamp)
	signature := SignMessage(c.keyManager, message)
	resp, err := http.PostForm(c.nftMarketURL+"/api/v1/collection/updateCollection",
		url.Values{
			"id":             {Id},
			"account_name":   {AccountName},
			"name":           {Name},
			"collection_url": {cp.CollectionUrl},
			"description":    {cp.Description},
			"category_id":    {CategoryId},
			"external_link":  {cp.ExternalLink},
			"twitter_link":   {cp.TwitterLink},
			"instagram_link": {cp.InstagramLink},
			"telegram_link":  {cp.TelegramLink},
			"discord_link":   {cp.DiscordLink},
			"logo_image":     {cp.LogoImage},
			"featured_image": {cp.FeaturedImage},
			"banner_image":   {cp.BannerImage},
			"timestamp":      {fmt.Sprintf("%d", timestamp)},
			"signature":      {signature}},
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(string(body))
	}
	result := &RespUpdateCollection{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *client) GetCollectionsByAccountIndex(AccountIndex int64) (*RespGetAccountCollections, error) {
	request_query := fmt.Sprintf("query MyQuery {\n  actionGetAccountCollections(account_index: %d) {\n    confirmedCollectionIdList\n    pendingCollections {\n      account_name\n      banner_image\n      banner_thumb\n      browse_count\n      category_id\n      created_at\n      creator_earning_rate\n      description\n      discord_link\n      expired_at\n      external_link\n      featured_Thumb\n      featured_image\n      floor_price\n      id\n      instagram_link\n      item_count\n      l2_collection_id\n      logo_image\n      logo_thumb\n      name\n      one_day_trade_volume\n      short_name\n      status\n      telegram_link\n      total_trade_volume\n      twitter_link\n    }\n  }\n}", AccountIndex)
	input := InputGetAccountCollectionsActionBody{AccountIndex: AccountIndex}
	action := ActionBody{Name: "actionGetAccountCollections"}
	SessionVariables := SessionVariablesBody{XHasuraUserId: "x-hasura-role", XHasuraRole: "admin"}
	req := ReqGetAccountCollections{
		Input:            input,
		Action:           action,
		SessionVariables: SessionVariables,
		RequestQuery:     request_query,
	}
	statusJSON, _ := json.Marshal(req)
	resp, err := http.Post(c.nftMarketURL+"/api/v1/action/actionGetAccountCollections", "application/json", bytes.NewReader(statusJSON))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(string(body))
	}
	result := &RespGetAccountCollections{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *client) MintNft(
	accountName string,
	CollectionId int64,
	NftUrl string, Name string,
	Description string, Media string,
	Properties string, Levels string, Stats string,
) (*RespCreateAsset, error) {

	ContentHash, err := calculateContentHash(accountName, CollectionId, Name, Properties, Levels, Stats)

	respPrepareTx, err := http.Get(c.nftMarketURL + fmt.Sprintf("/api/v1/preparetx/getPrepareMintNftTxInfo?account_name=%s&collection_id=%d&name=%s&content_hash=%s", accountName, CollectionId, Name, ContentHash))
	if err != nil {
		return nil, err
	}
	body, err := ioutil.ReadAll(respPrepareTx.Body)
	if err != nil {
		return nil, err
	}
	if respPrepareTx.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(string(body))
	}
	resultPrepare := &RespetPreparetxInfo{}
	if err := json.Unmarshal(body, &resultPrepare); err != nil {
		return nil, err
	}
	tx, err := PrepareMintNftTxInfo(c.keyManager, resultPrepare.Transtion)
	if err != nil {
		return nil, err
	}

	resp, err := http.PostForm(c.nftMarketURL+"/api/v1/asset/createAsset",
		url.Values{
			"collection_id": {fmt.Sprintf("%d", CollectionId)},
			"nft_url":       {NftUrl},
			"name":          {Name},
			"description":   {Description},
			"media":         {Media},
			"properties":    {Properties},
			"levels":        {Levels},
			"stats":         {Stats},
			"transaction":   {tx},
		},
	)

	defer resp.Body.Close()
	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(string(body))
	}
	result := &RespCreateAsset{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *client) GetNftByNftId(nftId int64) (*RespetAssetByAssetId, error) {
	request_query := fmt.Sprintf("query MyQuery {\n  actionGetAssetByAssetId(asset_id: %d) {\n    asset {\n      account_name\n      audio_thumb\n      collection_id\n      content_hash\n      created_at\n      creator_earning_rate\n      description\n      expired_at\n      id\n      image_thumb\n      levels\n      media\n      name\n      nft_index\n      properties\n      stats\n      status\n      video_thumb\n    }\n  }\n}\n", nftId)
	input := InputGetAssetByIdActionBody{AssetId: nftId}
	action := ActionBody{Name: "actionGetAssetByAssetId"}
	SessionVariables := SessionVariablesBody{XHasuraUserId: "x-hasura-role", XHasuraRole: "admin"}
	req := ReqGetAssetById{
		Input:            input,
		Action:           action,
		SessionVariables: SessionVariables,
		RequestQuery:     request_query,
	}
	statusJSON, _ := json.Marshal(req)
	resp, err := http.Post(c.nftMarketURL+"/api/v1/action/actionGetAssetByAssetId", "application/json", bytes.NewReader(statusJSON))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(string(body))
	}
	result := &RespetAssetByAssetId{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *client) TransferNft(
	AssetId int64,
	accountName string,
	toAccountName string) (*ResqSendTransferNft, error) {
	respPrepareTx, err := http.Get(c.nftMarketURL + fmt.Sprintf("/api/v1/preparetx/getPrepareTransferNftTxInfo?account_name=%s&to_account_name=%s&nft_id=%d", accountName, toAccountName, AssetId))
	if err != nil {
		return nil, err
	}
	body, err := ioutil.ReadAll(respPrepareTx.Body)
	if err != nil {
		return nil, err
	}
	if respPrepareTx.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(string(body))
	}

	resultPrepare := &RespetPreparetxInfo{}
	if err := json.Unmarshal(body, &resultPrepare); err != nil {
		return nil, err
	}
	txInfo, err := PrepareTransferNftTxInfo(c.keyManager, resultPrepare.Transtion)

	resp, err := http.PostForm(c.nftMarketURL+"/api/v1/asset/sendTransferNft",
		url.Values{
			"asset_id":    {fmt.Sprintf("%d", AssetId)},
			"transaction": {txInfo},
		},
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(string(body))
	}
	result := &ResqSendTransferNft{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *client) WithdrawNft(accountName string, AssetId int64) (*ResqSendWithdrawNft, error) {
	respPrepareTx, err := http.Get(c.nftMarketURL + fmt.Sprintf("/api/v1/preparetx/getPrepareWithdrawNftTxInfo?account_name=%s&nft_id=%d", accountName, AssetId))
	if err != nil {
		return nil, err
	}
	body, err := ioutil.ReadAll(respPrepareTx.Body)
	if err != nil {
		return nil, err
	}
	if respPrepareTx.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(string(body))
	}
	resultPrepare := &RespetPreparetxInfo{}
	if err := json.Unmarshal(body, &resultPrepare); err != nil {
		return nil, err
	}

	txInfo, err := PrepareWithdrawNftTxInfo(c.keyManager, resultPrepare.Transtion)
	resp, err := http.PostForm(c.nftMarketURL+"/api/v1/asset/sendWithdrawNft",
		url.Values{
			"asset_id":    {fmt.Sprintf("%d", AssetId)},
			"transaction": {txInfo},
		},
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(string(body))
	}
	result := &ResqSendWithdrawNft{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *client) SellNft(accountName string, AssetId int64, moneyType int64, AssetAmount *big.Int) (*RespListOffer, error) {
	respPrepareTx, err := http.Get(c.nftMarketURL + fmt.Sprintf("/api/v1/preparetx/getPrepareOfferTxInfo?account_name=%s&nft_id=%d&money_id=%d&money_amount=%d&is_sell=true", accountName, AssetId, moneyType, AssetAmount))
	if err != nil {
		return nil, err
	}
	body, err := ioutil.ReadAll(respPrepareTx.Body)
	if err != nil {
		return nil, err
	}
	if respPrepareTx.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(string(body))
	}
	resultPrepare := &RespetPreparetxInfo{}
	if err := json.Unmarshal(body, &resultPrepare); err != nil {
		return nil, err
	}

	tx, err := PrepareOfferTxInfo(c.keyManager, resultPrepare.Transtion, true)
	if err != nil {
		return nil, err
	}
	return c.Offer(accountName, tx)
}

func (c *client) BuyNft(accountName string, AssetId int64, moneyType int64, AssetAmount *big.Int) (*RespListOffer, error) {
	respPrepareTx, err := http.Get(c.nftMarketURL + fmt.Sprintf("/api/v1/preparetx/getPrepareOfferTxInfo?account_name=%s&nft_id=%d&money_id=%d&money_amount=%d&is_sell=false", accountName, AssetId, moneyType, AssetAmount))
	if err != nil {
		return nil, err
	}
	body, err := ioutil.ReadAll(respPrepareTx.Body)
	if err != nil {
		return nil, err
	}
	if respPrepareTx.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(string(body))
	}
	resultPrepare := &RespetPreparetxInfo{}
	if err := json.Unmarshal(body, &resultPrepare); err != nil {
		return nil, err
	}
	tx, err := PrepareOfferTxInfo(c.keyManager, resultPrepare.Transtion, false)
	if err != nil {
		return nil, err
	}
	return c.Offer(accountName, tx)
}

func (c *client) Offer(accountName string, tx string) (*RespListOffer, error) {
	resp, err := http.PostForm(c.nftMarketURL+"/api/v1/offer/listOffer",
		url.Values{
			"accountName": {accountName},
			"transaction": {tx},
		},
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(string(body))
	}
	result := &RespListOffer{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *client) GetNextOfferId(AccountName string) (*RespGetNextOfferId, error) {
	resp, err := http.Get(c.nftMarketURL + fmt.Sprintf("/api/v1/offer/getNextOfferId?account_name=%s", AccountName))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(string(body))
	}
	result := &RespGetNextOfferId{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *client) GetOfferById(OfferId int64) (*RespGetOfferByOfferId, error) {
	resp, err := http.Get(c.nftMarketURL + fmt.Sprintf("/api/v1/offer/getOfferByOfferId?offer_id=%d", OfferId))

	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(string(body))
	}
	result := &RespGetOfferByOfferId{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *client) AcceptOffer(accountName string, offerId int64, isSell bool, AssetAmount *big.Int) (*RespAcceptOffer, error) {
	respPrepareTx, err := http.Get(c.nftMarketURL + fmt.Sprintf("/api/v1/preparetx/getPrepareAtomicMatchWithTx?account_name=%s&offer_id=%d&money_id=%d&money_amount=%s&is_sell=%v", accountName, offerId, 0, AssetAmount.String(), isSell))
	if err != nil {
		return nil, err
	}
	body, err := ioutil.ReadAll(respPrepareTx.Body)
	if err != nil {
		return nil, err
	}
	if respPrepareTx.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(string(body))
	}
	resultPrepare := &RespetPreparetxInfo{}
	if err := json.Unmarshal(body, &resultPrepare); err != nil {
		return nil, err
	}

	txInfo, err := PrepareAtomicMatchWithTx(c.keyManager, resultPrepare.Transtion, isSell, AssetAmount)
	if err != nil {
		return nil, err
	}
	resp, err := http.PostForm(c.nftMarketURL+"/api/v1/offer/acceptOffer",
		url.Values{
			"id":          {fmt.Sprintf("%d", offerId)},
			"transaction": {txInfo},
		},
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(string(body))
	}
	result := &RespAcceptOffer{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *client) GetLayer2BasicInfo() (*RespGetLayer2BasicInfo, error) {
	resp, err := http.Get(c.legendURL + "/api/v1/info/getLayer2BasicInfo")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(string(body))
	}
	result := &RespGetLayer2BasicInfo{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func PrepareCreateCollectionTxInfo(key KeyManager, txInfoPrepare, Description string) (string, error) {
	txInfo := &CreateCollectionTxInfo{}
	err := json.Unmarshal([]byte(txInfoPrepare), txInfo)
	if err != nil {
		return "", err
	}
	//reset
	txInfo.Introduction = Description
	tx, err := ConstructCreateCollectionTx(key, txInfo) //sign tx message
	if err != nil {
		return "", err
	}
	return tx, nil
}

func PrepareMintNftTxInfo(key KeyManager, txInfoPrepare string) (string, error) {
	txInfo := &MintNftTxInfo{}
	err := json.Unmarshal([]byte(txInfoPrepare), txInfo)
	if err != nil {
		return "", err
	}
	tx, err := ConstructMintNftTx(key, txInfo)
	if err != nil {
		return "", err
	}
	return tx, nil
}

func PrepareTransferNftTxInfo(key KeyManager, txInfoPrepare string) (string, error) {
	txInfo := &TransferNftTxInfo{}
	err := json.Unmarshal([]byte(txInfoPrepare), txInfo)
	if err != nil {
		return "", err
	}
	tx, err := ConstructTransferNftTx(key, txInfo)
	if err != nil {
		return "", err
	}
	return tx, err
}

func PrepareAtomicMatchWithTx(key KeyManager, txInfoPrepare string, isSell bool, AssetAmount *big.Int) (string, error) {
	txInfo := &AtomicMatchTxInfo{}
	err := json.Unmarshal([]byte(txInfoPrepare), txInfo)
	if err != nil {
		return "", err
	}
	if !isSell {
		signedTx, err := ConstructOfferTx(key, txInfo.BuyOffer)
		if err != nil {
			return "", err
		}
		signedOffer, _ := ParseOfferTxInfo(signedTx)
		txInfo.BuyOffer = signedOffer
		txInfo.BuyOffer.AssetAmount = AssetAmount
	}
	if isSell {
		signedTx, err := ConstructOfferTx(key, txInfo.SellOffer)
		if err != nil {
			return "", err
		}
		signedOffer, _ := ParseOfferTxInfo(signedTx)
		txInfo.SellOffer = signedOffer
		txInfo.SellOffer.AssetAmount = AssetAmount
	}

	tx, err := ConstructAtomicMatchTx(key, txInfo)
	if err != nil {
		return "", err
	}
	return tx, err
}

func PrepareWithdrawNftTxInfo(key KeyManager, txInfoPrepare string) (string, error) {
	txInfo := &WithdrawNftTxInfo{}
	err := json.Unmarshal([]byte(txInfoPrepare), txInfo)
	if err != nil {
		return "", err
	}
	tx, err := ConstructWithdrawNftTx(key, txInfo)
	if err != nil {
		return "", err
	}
	return tx, err
}

func PrepareOfferTxInfo(key KeyManager, txInfoPrepare string, isSell bool) (string, error) {
	txInfo := &OfferTxInfo{}
	err := json.Unmarshal([]byte(txInfoPrepare), txInfo)
	if err != nil {
		return "", err
	}
	txInfo.Type = 0
	if isSell {
		txInfo.Type = 1
	}
	tx, err := ConstructOfferTx(key, txInfo)
	if err != nil {
		return "", err
	}
	return tx, err
}

func calculateContentHash(accountName string, collectionId int64, name string, _properties string, _levels string, _stats string) (string, error) {

	var (
		properties []Propertie
		levels     []Level
		stats      []Stat
	)
	err := json.Unmarshal([]byte(_properties), &properties)
	if err != nil {
		return "", fmt.Errorf("json Unmarshal err properties%s", _properties)
	}
	err = json.Unmarshal([]byte(_levels), &levels)
	if err != nil {
		return "", fmt.Errorf("json Unmarshal err levels%s", _levels)
	}
	err = json.Unmarshal([]byte(_stats), &stats)
	if err != nil {
		return "", fmt.Errorf("json Unmarshal err stats%s", _stats)
	}

	content := fmt.Sprintf("ACC:%s CID:%d NFT:%s", accountName, collectionId, name)

	if len(properties) == 0 {
		content = content + " PROPERTIES: empty"
	} else {
		content = content + " PROPERTIES: "
		m := make(map[string]string, 0)
		keys := make([]string, 0)
		for _, k := range properties {
			m[k.Name] = k.Value
			keys = append(keys, k.Name)
		}
		sort.Strings(keys)
		for _, k := range keys {
			content = content + fmt.Sprintf("k:%s v:%s", k, m[k])
		}
	}

	if len(levels) == 0 {
		content = content + " LEVELS: empty"
	} else {
		content = content + " LEVELS: "
		m := make(map[string]int64, 0)
		keys := make([]string, 0)
		for _, k := range levels {
			m[k.Name] = k.Value
			keys = append(keys, k.Name)
		}
		sort.Strings(keys)
		for _, k := range keys {
			content = content + fmt.Sprintf("k:%s v:%d", k, m[k])
		}
	}

	if len(stats) == 0 {
		content = content + " STATS: empty"
	} else {
		content = content + " STATS: "
		m := make(map[string]int64, 0)
		keys := make([]string, 0)
		for _, k := range stats {
			m[k.Name] = k.Value
			keys = append(keys, k.Name)
		}
		sort.Strings(keys)
		for _, k := range keys {
			content = content + fmt.Sprintf("k:%s v:%d", k, m[k])
		}
	}

	hFunc := mimc.NewMiMC()
	hFunc.Write([]byte(content))
	bytes := crypto.Keccak256Hash([]byte(content))
	fmt.Println("==nft content ==", content)
	return common.Bytes2Hex(bytes[:]), nil
}

func SignMessage(key KeyManager, message string) string {
	fmt.Println("message: ", message)
	sig, err := key.Sign([]byte(message), mimc.NewMiMC())
	if err != nil {
		panic("failed to sign message, err: " + err.Error())
	}

	signed := hex.EncodeToString(sig[:])
	fmt.Println("signed:", signed)
	return signed
}
