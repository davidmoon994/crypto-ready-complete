package service

import (
	"crypto-final/internal/model"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
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

// getBinanceBalance 获取币安USDC+USDT余额
func (ws *WalletService) getBinanceBalance(account *model.AdminAccount) (float64, error) {
	if account.APIKey == "" || account.APISecret == "" {
		return 0, fmt.Errorf("未配置Binance API Key")
	}

	// API参数
	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	queryString := "timestamp=" + timestamp

	// 生成签名
	signature := ws.binanceSign(queryString, account.APISecret)
	url := "https://api.binance.com/api/v3/account?" + queryString + "&signature=" + signature

	// 创建请求
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("X-MBX-APIKEY", account.APIKey)

	// 发送请求

	resp, err := ws.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("API请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("读取响应失败: %v", err)
	}

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("API返回错误 [%d]: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var result struct {
		Balances []struct {
			Asset  string `json:"asset"`
			Free   string `json:"free"`
			Locked string `json:"locked"`
		} `json:"balances"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("解析响应失败: %v", err)
	}

	// 计算USDC + USDT总余额
	totalBalance := 0.0
	for _, balance := range result.Balances {
		if balance.Asset == "USDC" || balance.Asset == "USDT" {
			free, _ := strconv.ParseFloat(balance.Free, 64)
			locked, _ := strconv.ParseFloat(balance.Locked, 64)
			assetBalance := free + locked
			totalBalance += assetBalance
			fmt.Printf("  Binance %s: %.2f (可用: %.2f, 锁定: %.2f)\n",
				balance.Asset, assetBalance, free, locked)
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

	// API参数
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	method := "GET"
	requestPath := "/api/v5/account/balance"

	// 生成签名
	preHash := timestamp + method + requestPath
	signature := ws.okxSign(preHash, account.APISecret)

	url := "https://www.okx.com" + requestPath

	// 创建请求
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return 0, fmt.Errorf("创建请求失败: %v", err)
	}

	// 设置请求头
	req.Header.Set("OK-ACCESS-KEY", account.APIKey)
	req.Header.Set("OK-ACCESS-SIGN", signature)
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	passphrase := account.Passphrase
	if passphrase == "" {
		return 0, fmt.Errorf("未配置OKX Passphrase")
	}
	req.Header.Set("OK-ACCESS-PASSPHRASE", passphrase)
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	resp, err := ws.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("API请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("读取响应失败: %v", err)
	}

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("API返回错误 [%d]: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			Details []struct {
				Ccy     string `json:"ccy"`
				CashBal string `json:"cashBal"`
			} `json:"details"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("解析响应失败: %v", err)
	}

	if result.Code != "0" {
		return 0, fmt.Errorf("OKX API错误: %s", result.Msg)
	}

	// 计算USDC + USDT总余额
	totalBalance := 0.0
	if len(result.Data) > 0 {
		for _, detail := range result.Data[0].Details {
			if detail.Ccy == "USDC" || detail.Ccy == "USDT" {
				balance, _ := strconv.ParseFloat(detail.CashBal, 64)
				totalBalance += balance
				fmt.Printf("  OKX %s: %.2f\n", detail.Ccy, balance)
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

	// 需要Etherscan API Key（可以免费申请）
	// 暂时使用API Secret字段存储Etherscan API Key
	etherscanAPIKey := account.APISecret
	if etherscanAPIKey == "" {
		etherscanAPIKey = "YourApiKeyToken" // 替换为你的Etherscan API Key
	}

	totalBalance := 0.0

	// 获取USDC余额（ERC20）
	usdcBalance, err := ws.getERC20Balance(
		account.WalletAddress,
		"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", // USDC合约地址
		etherscanAPIKey,
		6, // USDC精度
	)
	if err != nil {
		fmt.Printf("  ⚠️  获取USDC余额失败: %v\n", err)
	} else {
		fmt.Printf("  Wallet USDC: %.2f\n", usdcBalance)
		totalBalance += usdcBalance
	}

	// 获取USDT余额（ERC20）
	usdtBalance, err := ws.getERC20Balance(
		account.WalletAddress,
		"0xdAC17F958D2ee523a2206206994597C13D831ec7", // USDT合约地址
		etherscanAPIKey,
		6, // USDT精度
	)
	if err != nil {
		fmt.Printf("  ⚠️  获取USDT余额失败: %v\n", err)
	} else {
		fmt.Printf("  Wallet USDT: %.2f\n", usdtBalance)
		totalBalance += usdtBalance
	}

	if totalBalance == 0 && (err != nil) {
		return 0, fmt.Errorf("无法获取钱包余额")
	}

	return totalBalance, nil
}

// getERC20Balance 获取ERC20代币余额
func (ws *WalletService) getERC20Balance(address, contractAddress, apiKey string, decimals int) (float64, error) {
	url := fmt.Sprintf(
		"https://api.etherscan.io/api?module=account&action=tokenbalance&contractaddress=%s&address=%s&tag=latest&apikey=%s",
		contractAddress, address, apiKey,
	)

	resp, err := ws.httpClient.Get(url)
	if err != nil {
		return 0, fmt.Errorf("API请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("读取响应失败: %v", err)
	}

	var result struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Result  string `json:"result"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("解析响应失败: %v", err)
	}

	if result.Status != "1" {
		return 0, fmt.Errorf("Etherscan API错误: %s", result.Message)
	}

	// 解析余额（需要除以10^decimals）
	balanceInt, err := strconv.ParseInt(result.Result, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("解析余额失败: %v", err)
	}

	// 转换为实际余额
	divisor := 1.0
	for i := 0; i < decimals; i++ {
		divisor *= 10
	}
	balance := float64(balanceInt) / divisor

	return balance, nil
}
