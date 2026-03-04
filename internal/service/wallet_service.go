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
	"strings"
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

	// 🔥 只查询交易账户（已包含所有持仓、未实现盈亏和资金）
	balance, err := ws.getOKXTradingBalance(account)
	if err != nil {
		fmt.Printf("  ⚠️  获取OKX账户失败: %v\n", err)
		return 0, err
	}

	fmt.Printf("  ✓ OKX 总资产: $%.2f\n", balance)
	return balance, nil
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
				availEq, _ := strconv.ParseFloat(detail.AvailEq, 64)
				cashBal, _ := strconv.ParseFloat(detail.CashBal, 64)
				frozenBal, _ := strconv.ParseFloat(detail.FrozenBal, 64)
				ordFrozen, _ := strconv.ParseFloat(detail.OrdFrozen, 64)
				upl, _ := strconv.ParseFloat(detail.Upl, 64)

				if eq > 0 {
					totalBalance += eq
					fmt.Printf("  OKX %s: 总权益=$%.2f (可用=$%.2f, 现金=$%.2f, 冻结=$%.2f, 挂单=$%.2f, 未实现=$%.2f)\n",
						detail.Ccy, eq, availEq, cashBal, frozenBal, ordFrozen, upl)
				}
			}
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

// GetPositions 获取持仓列表
func (ws *WalletService) GetPositions(account *model.AdminAccount, limit int) ([]model.Position, error) {
	switch account.AccountType {
	case "Binance":
		return ws.getBinancePositions(account, limit)
	case "OKX":
		return ws.getOKXPositions(account, limit)
	default:
		return []model.Position{}, nil
	}
}

// GetOrders 获取当前委托
func (ws *WalletService) GetOrders(account *model.AdminAccount, limit int) ([]model.Order, error) {
	switch account.AccountType {
	case "Binance":
		return ws.getBinanceOrders(account, limit)
	case "OKX":
		return ws.getOKXOrders(account, limit)
	default:
		return []model.Order{}, nil
	}
}

// GetHistoryTrades 获取历史成交
func (ws *WalletService) GetHistoryTrades(account *model.AdminAccount, limit int) ([]model.HistoryTrade, error) {
	switch account.AccountType {
	case "Binance":
		return ws.getBinanceHistoryTrades(account, limit)
	case "OKX":
		return ws.getOKXHistoryTrades(account, limit)
	default:
		return []model.HistoryTrade{}, nil
	}
}

// getBinancePositions 获取Binance持仓
func (ws *WalletService) getBinancePositions(account *model.AdminAccount, limit int) ([]model.Position, error) {
	timestamp := fmt.Sprintf("%d", time.Now().UnixNano()/1000000)
	queryString := fmt.Sprintf("timestamp=%s", timestamp)
	signature := ws.binanceSign(queryString, account.APISecret)

	url := "https://fapi.binance.com/fapi/v2/positionRisk?" + queryString + "&signature=" + signature

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-MBX-APIKEY", account.APIKey)

	resp, err := ws.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API返回错误 [%d]: %s", resp.StatusCode, string(body))
	}

	var result []struct {
		Symbol           string `json:"symbol"`
		PositionAmt      string `json:"positionAmt"`
		EntryPrice       string `json:"entryPrice"`
		MarkPrice        string `json:"markPrice"`
		UnRealizedProfit string `json:"unRealizedProfit"`
		Leverage         string `json:"leverage"`
		MarginType       string `json:"marginType"`
		PositionSide     string `json:"positionSide"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	positions := []model.Position{}
	count := 0

	for _, pos := range result {
		posAmt, _ := strconv.ParseFloat(pos.PositionAmt, 64)

		// 跳过空仓
		if posAmt == 0 {
			continue
		}

		if count >= limit {
			break
		}

		entryPrice, _ := strconv.ParseFloat(pos.EntryPrice, 64)
		markPrice, _ := strconv.ParseFloat(pos.MarkPrice, 64)
		unrealizedPnl, _ := strconv.ParseFloat(pos.UnRealizedProfit, 64)
		leverage, _ := strconv.Atoi(pos.Leverage)

		side := "LONG"
		if posAmt < 0 {
			side = "SHORT"
			posAmt = -posAmt
		}

		pnlRate := 0.0
		if entryPrice > 0 {
			pnlRate = (unrealizedPnl / (posAmt * entryPrice)) * 100
		}

		positions = append(positions, model.Position{
			Symbol:            pos.Symbol,
			Side:              side,
			Size:              posAmt,
			EntryPrice:        entryPrice,
			MarkPrice:         markPrice,
			UnrealizedPnl:     unrealizedPnl,
			UnrealizedPnlRate: pnlRate,
			Leverage:          leverage,
			MarginType:        pos.MarginType,
		})

		count++
	}

	return positions, nil
}

// getBinanceOrders 获取Binance当前委托
func (ws *WalletService) getBinanceOrders(account *model.AdminAccount, limit int) ([]model.Order, error) {
	timestamp := fmt.Sprintf("%d", time.Now().UnixNano()/1000000)
	queryString := fmt.Sprintf("timestamp=%s", timestamp)
	signature := ws.binanceSign(queryString, account.APISecret)

	url := "https://fapi.binance.com/fapi/v1/openOrders?" + queryString + "&signature=" + signature

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-MBX-APIKEY", account.APIKey)

	resp, err := ws.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API返回错误 [%d]: %s", resp.StatusCode, string(body))
	}

	var result []struct {
		OrderID     int64  `json:"orderId"`
		Symbol      string `json:"symbol"`
		Side        string `json:"side"`
		Type        string `json:"type"`
		Price       string `json:"price"`
		OrigQty     string `json:"origQty"`
		ExecutedQty string `json:"executedQty"`
		Status      string `json:"status"`
		Time        int64  `json:"time"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	orders := []model.Order{}
	count := 0

	for _, ord := range result {
		if count >= limit {
			break
		}

		price, _ := strconv.ParseFloat(ord.Price, 64)
		origQty, _ := strconv.ParseFloat(ord.OrigQty, 64)
		executedQty, _ := strconv.ParseFloat(ord.ExecutedQty, 64)

		orders = append(orders, model.Order{
			OrderID:     fmt.Sprintf("%d", ord.OrderID),
			Symbol:      ord.Symbol,
			Side:        ord.Side,
			Type:        ord.Type,
			Price:       price,
			OrigQty:     origQty,
			ExecutedQty: executedQty,
			Status:      ord.Status,
			Time:        time.Unix(ord.Time/1000, 0).Format("2006-01-02 15:04:05"),
		})

		count++
	}

	return orders, nil
}

// getBinanceHistoryTrades 获取Binance历史成交
func (ws *WalletService) getBinanceHistoryTrades(account *model.AdminAccount, limit int) ([]model.HistoryTrade, error) {
	timestamp := fmt.Sprintf("%d", time.Now().UnixNano()/1000000)
	queryString := fmt.Sprintf("timestamp=%s&limit=%d", timestamp, limit)
	signature := ws.binanceSign(queryString, account.APISecret)

	url := "https://fapi.binance.com/fapi/v1/userTrades?" + queryString + "&signature=" + signature

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-MBX-APIKEY", account.APIKey)

	resp, err := ws.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API返回错误 [%d]: %s", resp.StatusCode, string(body))
	}

	var result []struct {
		Symbol      string `json:"symbol"`
		Side        string `json:"side"`
		Price       string `json:"price"`
		Qty         string `json:"qty"`
		RealizedPnl string `json:"realizedPnl"`
		Commission  string `json:"commission"`
		Time        int64  `json:"time"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	trades := []model.HistoryTrade{}

	for _, trade := range result {
		price, _ := strconv.ParseFloat(trade.Price, 64)
		qty, _ := strconv.ParseFloat(trade.Qty, 64)
		pnl, _ := strconv.ParseFloat(trade.RealizedPnl, 64)
		commission, _ := strconv.ParseFloat(trade.Commission, 64)

		tradeTime := time.Unix(trade.Time/1000, 0).Format("2006-01-02 15:04:05")

		trades = append(trades, model.HistoryTrade{
			Symbol:      trade.Symbol,
			Side:        trade.Side,
			OpenTime:    tradeTime,
			CloseTime:   tradeTime,
			OpenPrice:   price,
			ClosePrice:  price,
			Quantity:    qty,
			RealizedPnl: pnl,
			Commission:  commission,
		})
	}

	return trades, nil
}

// getOKXPositions 获取OKX持仓
func (ws *WalletService) getOKXPositions(account *model.AdminAccount, limit int) ([]model.Position, error) {
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	method := "GET"
	requestPath := "/api/v5/account/positions"
	body := ""

	message := timestamp + method + requestPath + body
	signature := ws.okxSign(message, account.APISecret)

	url := "https://www.okx.com" + requestPath

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("OK-ACCESS-KEY", account.APIKey)
	req.Header.Set("OK-ACCESS-SIGN", signature)
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("OK-ACCESS-PASSPHRASE", account.Passphrase)
	req.Header.Set("Content-Type", "application/json")

	resp, err := ws.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API返回错误 [%d]: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			InstID   string `json:"instId"`   // 产品ID
			PosSide  string `json:"posSide"`  // 持仓方向
			Pos      string `json:"pos"`      // 持仓数量
			AvgPx    string `json:"avgPx"`    // 开仓均价
			MarkPx   string `json:"markPx"`   // 标记价格
			Upl      string `json:"upl"`      // 未实现盈亏
			UplRatio string `json:"uplRatio"` // 未实现盈亏比率
			Lever    string `json:"lever"`    // 杠杆倍数
			MgnMode  string `json:"mgnMode"`  // 保证金模式
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	if result.Code != "0" {
		return nil, fmt.Errorf("OKX API错误 [%s]: %s", result.Code, result.Msg)
	}

	positions := []model.Position{}
	count := 0

	for _, pos := range result.Data {
		posSize, _ := strconv.ParseFloat(pos.Pos, 64)

		// 跳过空仓
		if posSize == 0 {
			continue
		}

		if count >= limit {
			break
		}

		avgPx, _ := strconv.ParseFloat(pos.AvgPx, 64)
		markPx, _ := strconv.ParseFloat(pos.MarkPx, 64)
		upl, _ := strconv.ParseFloat(pos.Upl, 64)
		uplRatio, _ := strconv.ParseFloat(pos.UplRatio, 64)
		lever, _ := strconv.Atoi(pos.Lever)

		side := "LONG"
		if pos.PosSide == "short" {
			side = "SHORT"
		}

		marginType := "cross"
		if pos.MgnMode == "isolated" {
			marginType = "isolated"
		}

		positions = append(positions, model.Position{
			Symbol:            pos.InstID,
			Side:              side,
			Size:              posSize,
			EntryPrice:        avgPx,
			MarkPrice:         markPx,
			UnrealizedPnl:     upl,
			UnrealizedPnlRate: uplRatio * 100, // 转换为百分比
			Leverage:          lever,
			MarginType:        marginType,
		})

		count++
	}

	return positions, nil
}

// getOKXOrders 获取OKX当前委托
func (ws *WalletService) getOKXOrders(account *model.AdminAccount, limit int) ([]model.Order, error) {
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	method := "GET"
	requestPath := "/api/v5/trade/orders-pending"
	body := ""

	message := timestamp + method + requestPath + body
	signature := ws.okxSign(message, account.APISecret)

	url := "https://www.okx.com" + requestPath

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("OK-ACCESS-KEY", account.APIKey)
	req.Header.Set("OK-ACCESS-SIGN", signature)
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("OK-ACCESS-PASSPHRASE", account.Passphrase)
	req.Header.Set("Content-Type", "application/json")

	resp, err := ws.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API返回错误 [%d]: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			OrdID     string `json:"ordId"`     // 订单ID
			InstID    string `json:"instId"`    // 产品ID
			Side      string `json:"side"`      // 订单方向
			OrdType   string `json:"ordType"`   // 订单类型
			Px        string `json:"px"`        // 委托价格
			Sz        string `json:"sz"`        // 委托数量
			AccFillSz string `json:"accFillSz"` // 已成交数量
			State     string `json:"state"`     // 订单状态
			CTime     string `json:"cTime"`     // 创建时间
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	if result.Code != "0" {
		return nil, fmt.Errorf("OKX API错误 [%s]: %s", result.Code, result.Msg)
	}

	orders := []model.Order{}
	count := 0

	for _, ord := range result.Data {
		if count >= limit {
			break
		}

		px, _ := strconv.ParseFloat(ord.Px, 64)
		sz, _ := strconv.ParseFloat(ord.Sz, 64)
		accFillSz, _ := strconv.ParseFloat(ord.AccFillSz, 64)

		// 转换时间戳
		cTimeInt, _ := strconv.ParseInt(ord.CTime, 10, 64)
		orderTime := time.Unix(cTimeInt/1000, 0).Format("2006-01-02 15:04:05")

		orders = append(orders, model.Order{
			OrderID:     ord.OrdID,
			Symbol:      ord.InstID,
			Side:        strings.ToUpper(ord.Side),
			Type:        strings.ToUpper(ord.OrdType),
			Price:       px,
			OrigQty:     sz,
			ExecutedQty: accFillSz,
			Status:      ord.State,
			Time:        orderTime,
		})

		count++
	}

	return orders, nil
}

// getOKXHistoryTrades 获取OKX历史成交
func (ws *WalletService) getOKXHistoryTrades(account *model.AdminAccount, limit int) ([]model.HistoryTrade, error) {
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	method := "GET"
	requestPath := fmt.Sprintf("/api/v5/trade/orders-history?limit=%d", limit)
	body := ""

	message := timestamp + method + requestPath + body
	signature := ws.okxSign(message, account.APISecret)

	url := "https://www.okx.com" + requestPath

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("OK-ACCESS-KEY", account.APIKey)
	req.Header.Set("OK-ACCESS-SIGN", signature)
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("OK-ACCESS-PASSPHRASE", account.Passphrase)
	req.Header.Set("Content-Type", "application/json")

	resp, err := ws.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API返回错误 [%d]: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			InstID string `json:"instId"` // 产品ID
			Side   string `json:"side"`   // 订单方向
			Px     string `json:"px"`     // 委托价格
			AvgPx  string `json:"avgPx"`  // 成交均价
			Sz     string `json:"sz"`     // 委托数量
			Pnl    string `json:"pnl"`    // 收益
			Fee    string `json:"fee"`    // 手续费
			CTime  string `json:"cTime"`  // 创建时间
			UTime  string `json:"uTime"`  // 更新时间
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	if result.Code != "0" {
		return nil, fmt.Errorf("OKX API错误 [%s]: %s", result.Code, result.Msg)
	}

	trades := []model.HistoryTrade{}

	for _, trade := range result.Data {
		px, _ := strconv.ParseFloat(trade.Px, 64)
		avgPx, _ := strconv.ParseFloat(trade.AvgPx, 64)
		sz, _ := strconv.ParseFloat(trade.Sz, 64)
		pnl, _ := strconv.ParseFloat(trade.Pnl, 64)
		fee, _ := strconv.ParseFloat(trade.Fee, 64)

		// 转换时间戳
		cTimeInt, _ := strconv.ParseInt(trade.CTime, 10, 64)
		uTimeInt, _ := strconv.ParseInt(trade.UTime, 10, 64)

		openTime := time.Unix(cTimeInt/1000, 0).Format("2006-01-02 15:04:05")
		closeTime := time.Unix(uTimeInt/1000, 0).Format("2006-01-02 15:04:05")

		trades = append(trades, model.HistoryTrade{
			Symbol:      trade.InstID,
			Side:        strings.ToUpper(trade.Side),
			OpenTime:    openTime,
			CloseTime:   closeTime,
			OpenPrice:   px,
			ClosePrice:  avgPx,
			Quantity:    sz,
			RealizedPnl: pnl,
			Commission:  fee,
		})
	}

	return trades, nil
}
