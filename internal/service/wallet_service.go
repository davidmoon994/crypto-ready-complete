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
	fmt.Println("  [调试] 正在获取Binance现货账户...")
	spotBalance, err := ws.getBinanceSpotBalance(account)
	if err != nil {
		fmt.Printf("  ⚠️  获取Binance现货余额失败: %v\n", err)
	} else {
		totalBalance += spotBalance
		if spotBalance > 0 {
			fmt.Printf("  Binance 现货账户: $%.2f\n", spotBalance)
		}
	}

	// 2. 获取USDⓈ-M永续合约余额（智能合约）
	fmt.Println("  [调试] 正在获取Binance USDⓈ-M合约...")
	futuresBalance, err := ws.getBinanceFuturesBalance(account)
	if err != nil {
		fmt.Printf("  ⚠️  获取Binance USDⓈ-M合约失败: %v\n", err)
	} else {
		totalBalance += futuresBalance
		if futuresBalance > 0 {
			fmt.Printf("  Binance USDⓈ-M合约: $%.2f\n", futuresBalance)
		}
	}

	// 3. 获取币本位合约余额
	fmt.Println("  [调试] 正在获取Binance币本位合约...")
	coinFuturesBalance, err := ws.getBinanceCoinFuturesBalance(account)
	if err != nil {
		fmt.Printf("  ⚠️  获取Binance币本位合约失败: %v\n", err)
	} else {
		totalBalance += coinFuturesBalance
		if coinFuturesBalance > 0 {
			fmt.Printf("  Binance 币本位合约: $%.2f\n", coinFuturesBalance)
		}
	}

	fmt.Printf("  ✓ Binance 总余额: $%.2f\n", totalBalance)
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

	totalBalance := 0.0

	// 1. 获取交易账户余额（包含现货和杠杆）
	tradingBalance, err := ws.getOKXTradingBalance(account)
	if err != nil {
		fmt.Printf("  ⚠️  获取OKX交易账户失败: %v\n", err)
	} else {
		totalBalance += tradingBalance
		if tradingBalance > 0 {
			fmt.Printf("  OKX 交易账户: $%.2f\n", tradingBalance)
		}
	}

	// 2. 获取资金账户余额
	fundingBalance, err := ws.getOKXFundingBalance(account)
	if err != nil {
		fmt.Printf("  ⚠️  获取OKX资金账户失败: %v\n", err)
	} else {
		totalBalance += fundingBalance
		if fundingBalance > 0 {
			fmt.Printf("  OKX 资金账户: $%.2f\n", fundingBalance)
		}
	}

	// 3. 获取统一账户余额（包含所有持仓和未实现盈亏）
	unifiedBalance, err := ws.getOKXUnifiedBalance(account)
	if err != nil {
		fmt.Printf("  ⚠️  获取OKX统一账户失败: %v\n", err)
	} else {
		totalBalance += unifiedBalance
		if unifiedBalance > 0 {
			fmt.Printf("  OKX 统一账户: $%.2f\n", unifiedBalance)
		}
	}

	fmt.Printf("  ✓ OKX 总资产: $%.2f\n", totalBalance)
	return totalBalance, nil
}

// getOKXTradingBalance 获取交易账户余额
func (ws *WalletService) getOKXTradingBalance(account *model.AdminAccount) (float64, error) {
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
	req.Header.Set("OK-ACCESS-PASSPHRASE", account.Passphrase)
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

	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			TotalEq string `json:"totalEq"` // 美元总权益
			Details []struct {
				Ccy       string `json:"ccy"`
				Eq        string `json:"eq"`        // 币种总权益
				AvailEq   string `json:"availEq"`   // 可用权益
				CashBal   string `json:"cashBal"`   // 现金余额
				FrozenBal string `json:"frozenBal"` // 冻结余额
				OrdFrozen string `json:"ordFrozen"` // 挂单冻结
				Upl       string `json:"upl"`       // 未实现盈亏
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
		for _, detail := range result.Data[0].Details {
			if detail.Ccy == "USDC" || detail.Ccy == "USDT" {
				eq, _ := strconv.ParseFloat(detail.Eq, 64)
				cashBal, _ := strconv.ParseFloat(detail.CashBal, 64)
				frozenBal, _ := strconv.ParseFloat(detail.FrozenBal, 64)
				ordFrozen, _ := strconv.ParseFloat(detail.OrdFrozen, 64)
				upl, _ := strconv.ParseFloat(detail.Upl, 64)

				if eq > 0 {
					totalBalance += eq
					fmt.Printf("    交易-%s: 权益=$%.2f (现金=$%.2f, 冻结=$%.2f, 挂单=$%.2f, 未实现=$%.2f)\n",
						detail.Ccy, eq, cashBal, frozenBal, ordFrozen, upl)
				}
			}
		}
	}

	return totalBalance, nil
}

// getOKXFundingBalance 获取资金账户余额
func (ws *WalletService) getOKXFundingBalance(account *model.AdminAccount) (float64, error) {
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	method := "GET"
	requestPath := "/api/v5/asset/balances"
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
	req.Header.Set("OK-ACCESS-PASSPHRASE", account.Passphrase)
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

	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			Ccy       string `json:"ccy"`
			Bal       string `json:"bal"`       // 余额
			FrozenBal string `json:"frozenBal"` // 冻结
			AvailBal  string `json:"availBal"`  // 可用
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, err
	}

	if result.Code != "0" {
		return 0, fmt.Errorf("API返回错误 [%s]: %s", result.Code, result.Msg)
	}

	totalBalance := 0.0

	for _, item := range result.Data {
		if item.Ccy == "USDC" || item.Ccy == "USDT" {
			bal, _ := strconv.ParseFloat(item.Bal, 64)
			frozenBal, _ := strconv.ParseFloat(item.FrozenBal, 64)
			availBal, _ := strconv.ParseFloat(item.AvailBal, 64)

			if bal > 0 {
				totalBalance += bal
				fmt.Printf("    资金-%s: 总额=$%.2f (可用=$%.2f, 冻结=$%.2f)\n",
					item.Ccy, bal, availBal, frozenBal)
			}
		}
	}

	return totalBalance, nil
}

// getOKXUnifiedBalance 获取统一账户余额（包含持仓）
func (ws *WalletService) getOKXUnifiedBalance(account *model.AdminAccount) (float64, error) {
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
	req.Header.Set("OK-ACCESS-PASSPHRASE", account.Passphrase)
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

	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			TotalEq     string `json:"totalEq"`     // 美元层面权益
			IsoEq       string `json:"isoEq"`       // 逐仓权益
			AdjEq       string `json:"adjEq"`       // 有效保证金
			NotionalUsd string `json:"notionalUsd"` // 持仓美元价值
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, err
	}

	if result.Code != "0" {
		return 0, fmt.Errorf("API返回错误 [%s]: %s", result.Code, result.Msg)
	}

	// 使用totalEq作为统一账户总权益
	if len(result.Data) > 0 {
		totalEq, _ := strconv.ParseFloat(result.Data[0].TotalEq, 64)
		notional, _ := strconv.ParseFloat(result.Data[0].NotionalUsd, 64)

		if totalEq > 0 || notional > 0 {
			fmt.Printf("    统一账户: 总权益=$%.2f (持仓价值=$%.2f)\n", totalEq, notional)
		}

		return totalEq, nil
	}

	return 0, nil
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
		fmt.Println("  ⚠️  未配置Etherscan API Key，将使用免费配额")
		etherscanAPIKey = "YourEtherscanAPIKey" // 替换为有效的Key
	}

	fmt.Printf("  [调试] 钱包地址: %s\n", account.WalletAddress)
	fmt.Printf("  [调试] Etherscan API Key: %s...%s\n", etherscanAPIKey[:min(4, len(etherscanAPIKey))], etherscanAPIKey[max(0, len(etherscanAPIKey)-4):])

	// USDC合约地址
	usdcContract := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	// USDT合约地址
	usdtContract := "0xdAC17F958D2ee523a2206206994597C13D831ec7"

	// 获取USDC余额
	fmt.Println("  [调试] 正在获取链上USDC余额...")
	usdcBalance, err := ws.getERC20Balance(account.WalletAddress, usdcContract, etherscanAPIKey, 6)
	if err != nil {
		fmt.Printf("  ⚠️  获取链上USDC余额失败: %v\n", err)
		usdcBalance = 0
	} else if usdcBalance > 0 {
		fmt.Printf("  链上钱包 USDC: $%.2f\n", usdcBalance)
	}

	// 获取USDT余额
	fmt.Println("  [调试] 正在获取链上USDT余额...")
	usdtBalance, err := ws.getERC20Balance(account.WalletAddress, usdtContract, etherscanAPIKey, 6)
	if err != nil {
		fmt.Printf("  ⚠️  获取链上USDT余额失败: %v\n", err)
		usdtBalance = 0
	} else if usdtBalance > 0 {
		fmt.Printf("  链上钱包 USDT: $%.2f\n", usdtBalance)
	}

	totalBalance := usdcBalance + usdtBalance

	if totalBalance > 0 {
		fmt.Printf("  ✓ 链上钱包 总余额: $%.2f\n", totalBalance)
	}

	// 如果两个都失败了，返回错误
	if usdcBalance == 0 && usdtBalance == 0 && err != nil {
		return 0, fmt.Errorf("无法获取钱包余额")
	}

	return totalBalance, nil
}

// 辅助函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// getERC20Balance 获取ERC20代币余额
func (ws *WalletService) getERC20Balance(walletAddress, contractAddress, apiKey string, decimals int) (float64, error) {
	url := fmt.Sprintf(
		"https://api.etherscan.io/api?module=account&action=tokenbalance&contractaddress=%s&address=%s&tag=latest&apikey=%s",
		contractAddress, walletAddress, apiKey,
	)

	fmt.Printf("  [调试] 请求URL: %s\n", url)

	resp, err := ws.httpClient.Get(url)
	if err != nil {
		return 0, fmt.Errorf("HTTP请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("读取响应失败: %v", err)
	}

	fmt.Printf("  [调试] Etherscan响应: %s\n", string(body))

	var result struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Result  string `json:"result"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("解析JSON失败: %v", err)
	}

	if result.Status != "1" {
		return 0, fmt.Errorf("Etherscan API错误: %s - %s", result.Message, result.Result)
	}

	// 使用big.Int处理大数字
	balance := new(big.Int)
	balance.SetString(result.Result, 10)

	// 根据小数位数转换
	divisor := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	balanceFloat := new(big.Float).SetInt(balance)
	balanceFloat.Quo(balanceFloat, divisor)

	floatBalance, _ := balanceFloat.Float64()
	return floatBalance, nil
}
