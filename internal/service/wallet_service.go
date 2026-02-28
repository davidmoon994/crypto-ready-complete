package service

import (
	"crypto-final/internal/model"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big" // 添加这行
	"net/http"
	"strconv"
	"time"
)

type WalletService struct {
	httpClient *http.Client
}

func NewWalletService() *WalletService {
	return &WalletService{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetBalance 获取钱包余额（自动识别类型）
func (ws *WalletService) GetBalance(account *model.AdminAccount) (float64, error) {
	switch account.AccountType {
	case "Binance":
		return ws.getBinanceBalance(account)
	case "OKX":
		return ws.getOKXBalance(account)
	case "Wallet":
		return ws.getWalletBalance(account)
	default:
		return 0, fmt.Errorf("不支持的账户类型: %s", account.AccountType)
	}
}

// ==================== Binance API ====================

func (ws *WalletService) getBinanceBalance(account *model.AdminAccount) (float64, error) {
	if account.APIKey == "" || account.APISecret == "" {
		return 0, fmt.Errorf("未配置Binance API Key")
	}

	totalBalance := 0.0

	// 1. 获取现货账户余额
	spotBalance, err := ws.getBinanceSpotBalance(account)
	if err != nil {
		fmt.Printf("⚠️  获取Binance现货余额失败: %v\n", err)
	} else {
		totalBalance += spotBalance
		fmt.Printf("  Binance 现货账户: $%.2f\n", spotBalance)
	}

	// 2. 获取合约账户余额 (USDT-M 永续合约)
	futuresBalance, err := ws.getBinanceFuturesBalance(account)
	if err != nil {
		fmt.Printf("⚠️  获取Binance合约余额失败: %v\n", err)
	} else {
		totalBalance += futuresBalance
		fmt.Printf("  Binance 合约账户: $%.2f\n", futuresBalance)
	}

	// 3. 获取币本位合约余额 (COIN-M 永续合约)
	coinFuturesBalance, err := ws.getBinanceCoinFuturesBalance(account)
	if err != nil {
		fmt.Printf("⚠️  获取Binance币本位合约余额失败: %v\n", err)
	} else {
		totalBalance += coinFuturesBalance
		fmt.Printf("  Binance 币本位合约: $%.2f\n", coinFuturesBalance)
	}

	fmt.Printf("  Binance 总余额: $%.2f\n", totalBalance)
	return totalBalance, nil
}

// getBinanceSpotBalance 获取现货账户余额
func (ws *WalletService) getBinanceSpotBalance(account *model.AdminAccount) (float64, error) {
	timestamp := fmt.Sprintf("%d", time.Now().UnixNano()/1000000)
	queryString := fmt.Sprintf("timestamp=%s", timestamp)
	signature := ws.binanceSign(queryString, account.APISecret)

	url := "https://api.binance.com/api/v3/account?" + queryString + "&signature=" + signature

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}

	req.Header.Set("X-MBX-APIKEY", account.APIKey)

	resp, err := ws.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("API返回错误 [%d]: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Balances []struct {
			Asset  string `json:"asset"`
			Free   string `json:"free"`
			Locked string `json:"locked"`
		} `json:"balances"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	totalBalance := 0.0
	for _, balance := range result.Balances {
		if balance.Asset == "USDC" || balance.Asset == "USDT" {
			free, _ := strconv.ParseFloat(balance.Free, 64)
			locked, _ := strconv.ParseFloat(balance.Locked, 64)
			assetTotal := free + locked

			if assetTotal > 0 {
				totalBalance += assetTotal
				fmt.Printf("    现货 %s: %.2f (可用: %.2f, 锁定: %.2f)\n",
					balance.Asset, assetTotal, free, locked)
			}
		}
	}

	return totalBalance, nil
}

// getBinanceFuturesBalance 获取USDT永续合约账户余额
func (ws *WalletService) getBinanceFuturesBalance(account *model.AdminAccount) (float64, error) {
	timestamp := fmt.Sprintf("%d", time.Now().UnixNano()/1000000)
	queryString := fmt.Sprintf("timestamp=%s", timestamp)
	signature := ws.binanceSign(queryString, account.APISecret)

	url := "https://fapi.binance.com/fapi/v2/balance?" + queryString + "&signature=" + signature

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}

	req.Header.Set("X-MBX-APIKEY", account.APIKey)

	resp, err := ws.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("API返回错误 [%d]: %s", resp.StatusCode, string(body))
	}

	var result []struct {
		Asset              string `json:"asset"`
		Balance            string `json:"balance"`
		CrossWalletBalance string `json:"crossWalletBalance"`
		CrossUnPnl         string `json:"crossUnPnl"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	totalBalance := 0.0
	// 同时统计USDT和USDC
	for _, balance := range result {
		if balance.Asset == "USDC" || balance.Asset == "USDT" {
			// 钱包余额
			walletBalance, _ := strconv.ParseFloat(balance.CrossWalletBalance, 64)
			// 未实现盈亏
			unrealizedPnl, _ := strconv.ParseFloat(balance.CrossUnPnl, 64)
			// 总权益 = 钱包余额 + 未实现盈亏
			equity := walletBalance + unrealizedPnl

			if equity > 0 || walletBalance > 0 || unrealizedPnl != 0 {
				totalBalance += equity
				fmt.Printf("    合约 %s: %.2f (钱包: %.2f, 未实现: %.2f)\n",
					balance.Asset, equity, walletBalance, unrealizedPnl)
			}
		}
	}

	return totalBalance, nil
}

// getBinanceCoinFuturesBalance 获取币本位永续合约账户余额
func (ws *WalletService) getBinanceCoinFuturesBalance(account *model.AdminAccount) (float64, error) {
	timestamp := fmt.Sprintf("%d", time.Now().UnixNano()/1000000)
	queryString := fmt.Sprintf("timestamp=%s", timestamp)
	signature := ws.binanceSign(queryString, account.APISecret)

	url := "https://dapi.binance.com/dapi/v1/balance?" + queryString + "&signature=" + signature

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}

	req.Header.Set("X-MBX-APIKEY", account.APIKey)

	resp, err := ws.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	if resp.StatusCode != 200 {
		// 如果没有币本位合约权限，返回0而不是错误
		if resp.StatusCode == 400 || resp.StatusCode == 401 {
			return 0, nil
		}
		return 0, fmt.Errorf("API返回错误 [%d]: %s", resp.StatusCode, string(body))
	}

	var result []struct {
		Asset              string `json:"asset"`
		Balance            string `json:"balance"`
		CrossWalletBalance string `json:"crossWalletBalance"`
		CrossUnPnl         string `json:"crossUnPnl"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	// 统计USDT和USDC（币本位合约也可能有稳定币）
	totalBalance := 0.0
	for _, balance := range result {
		if balance.Asset == "USDT" || balance.Asset == "USDC" {
			walletBalance, _ := strconv.ParseFloat(balance.CrossWalletBalance, 64)
			unrealizedPnl, _ := strconv.ParseFloat(balance.CrossUnPnl, 64)
			equity := walletBalance + unrealizedPnl

			if equity > 0 || walletBalance > 0 || unrealizedPnl != 0 {
				totalBalance += equity
				fmt.Printf("    币本位合约 %s: %.2f (钱包: %.2f, 未实现: %.2f)\n",
					balance.Asset, equity, walletBalance, unrealizedPnl)
			}
		}
	}

	return totalBalance, nil
}

// binanceSign 生成Binance签名
func (ws *WalletService) binanceSign(queryString, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(queryString))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// ==================== OKX API ====================

// getOKXBalance 获取OKX USDC+USDT余额
func (ws *WalletService) getOKXBalance(account *model.AdminAccount) (float64, error) {
	if account.APIKey == "" || account.APISecret == "" {
		return 0, fmt.Errorf("未配置OKX API Key")
	}

	passphrase := account.Passphrase
	if passphrase == "" {
		return 0, fmt.Errorf("未配置OKX Passphrase")
	}

	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	method := "GET"
	requestPath := "/api/v5/account/balance"
	body := ""

	message := timestamp + method + requestPath + body
	signature := ws.okxSign(message, account.APISecret)

	url := "https://www.okx.com" + requestPath

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return 0, err
	}

	req.Header.Set("OK-ACCESS-KEY", account.APIKey)
	req.Header.Set("OK-ACCESS-SIGN", signature)
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("OK-ACCESS-PASSPHRASE", passphrase)
	req.Header.Set("Content-Type", "application/json")

	resp, err := ws.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("API返回错误 [%d]: %s", resp.StatusCode, string(respBody))
	}

	// 打印原始响应用于调试
	fmt.Printf("  [调试] OKX API响应: %s\n", string(respBody))

	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			TotalEq string `json:"totalEq"` // 总权益（USD）
			Details []struct {
				Ccy       string `json:"ccy"`       // 币种
				Eq        string `json:"eq"`        // 币种总权益
				AvailEq   string `json:"availEq"`   // 可用权益
				CashBal   string `json:"cashBal"`   // 现金余额
				FrozenBal string `json:"frozenBal"` // 冻结余额
				UplRatio  string `json:"uplRatio"`  // 未实现盈亏比率
				Upl       string `json:"upl"`       // 未实现盈亏
				IsoUpl    string `json:"isoUpl"`    // 逐仓未实现盈亏
				MgnRatio  string `json:"mgnRatio"`  // 保证金率
			} `json:"details"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, err
	}

	if result.Code != "0" {
		return 0, fmt.Errorf("API返回错误 [%s]: %s", result.Code, result.Msg)
	}

	totalBalance := 0.0

	if len(result.Data) > 0 {
		// 统计USDC和USDT的权益
		for _, detail := range result.Data[0].Details {
			if detail.Ccy == "USDC" || detail.Ccy == "USDT" {
				eq, _ := strconv.ParseFloat(detail.Eq, 64)
				availEq, _ := strconv.ParseFloat(detail.AvailEq, 64)
				cashBal, _ := strconv.ParseFloat(detail.CashBal, 64)
				frozenBal, _ := strconv.ParseFloat(detail.FrozenBal, 64)
				upl, _ := strconv.ParseFloat(detail.Upl, 64)

				// eq应该是总权益，包含持仓和未实现盈亏
				if eq > 0 {
					totalBalance += eq
					fmt.Printf("  OKX %s: 总权益=%.2f (可用=%.2f, 现金=%.2f, 冻结=%.2f, 未实现=%.2f)\n",
						detail.Ccy, eq, availEq, cashBal, frozenBal, upl)
				}
			}
		}

		if totalBalance > 0 {
			fmt.Printf("  OKX 总权益: $%.2f\n", totalBalance)
		}
	}

	return totalBalance, nil
}

// okxSign 生成OKX签名
func (ws *WalletService) okxSign(message, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// ==================== 区块链钱包（Etherscan） ====================

// getWalletBalance 获取区块链钱包USDC+USDT余额
func (ws *WalletService) getWalletBalance(account *model.AdminAccount) (float64, error) {
	if account.WalletAddress == "" {
		return 0, fmt.Errorf("未配置钱包地址")
	}

	// API Key存储在APISecret字段
	etherscanAPIKey := account.APISecret
	if etherscanAPIKey == "" {
		etherscanAPIKey = "YourEtherscanAPIKey" // 备用Key
	}

	// USDC合约地址
	usdcContract := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	// USDT合约地址
	usdtContract := "0xdAC17F958D2ee523a2206206994597C13D831ec7"

	// 获取USDC余额
	usdcBalance, err := ws.getERC20Balance(account.WalletAddress, usdcContract, etherscanAPIKey, 6)
	if err != nil {
		fmt.Printf("⚠️  获取链上USDC余额失败: %v\n", err)
		usdcBalance = 0
	}

	// 获取USDT余额
	usdtBalance, err := ws.getERC20Balance(account.WalletAddress, usdtContract, etherscanAPIKey, 6)
	if err != nil {
		fmt.Printf("⚠️  获取链上USDT余额失败: %v\n", err)
		usdtBalance = 0
	}

	totalBalance := usdcBalance + usdtBalance

	// 详细显示
	if usdcBalance > 0 {
		fmt.Printf("  链上钱包 USDC: %.2f\n", usdcBalance)
	}
	if usdtBalance > 0 {
		fmt.Printf("  链上钱包 USDT: %.2f\n", usdtBalance)
	}
	if totalBalance > 0 {
		fmt.Printf("  链上钱包 总余额: $%.2f\n", totalBalance)
	}

	return totalBalance, nil
}

// getERC20Balance 获取ERC20代币余额
func (ws *WalletService) getERC20Balance(walletAddress, contractAddress, apiKey string, decimals int) (float64, error) {
	url := fmt.Sprintf(
		"https://api.etherscan.io/api?module=account&action=tokenbalance&contractaddress=%s&address=%s&tag=latest&apikey=%s",
		contractAddress, walletAddress, apiKey,
	)

	resp, err := ws.httpClient.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var result struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Result  string `json:"result"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	if result.Status != "1" {
		return 0, fmt.Errorf("API返回错误: %s", result.Message)
	}

	// 使用big.Int处理大数字（推荐方法）
	balance := new(big.Int)
	balance.SetString(result.Result, 10)

	// 根据小数位数转换
	divisor := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	balanceFloat := new(big.Float).SetInt(balance)
	balanceFloat.Quo(balanceFloat, divisor)

	floatBalance, _ := balanceFloat.Float64()
	return floatBalance, nil
}
