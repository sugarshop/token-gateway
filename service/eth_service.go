package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/sugarshop/token-gateway/model"
	"github.com/sugarshop/token-gateway/remote"
)

// ETHService ETH Transactions data parser service.
type ETHService struct {
	recentBlockNumer int64 // the most recent block number I have ever oberve.
	addrRWMutex sync.RWMutex
	subAddrs map[string]bool
	txRWMutex sync.RWMutex
	transactions map[string][]*model.ETHTransaction
}

var (
	eTHServiceInstance *ETHService
	eTHServiceOnce sync.Once
)

// ETHServiceInstance ETHService singleton
func ETHServiceInstance() *ETHService {
	eTHServiceOnce.Do(func() {
		eTHServiceInstance = &ETHService{
			subAddrs:   map[string]bool{},
			transactions:  map[string][]*model.ETHTransaction{},
		}
		ctx := context.Background()
		dec, err := remote.ETHRPCServiceInstance().ETHBlockDecimalNumber(ctx)
		if err != nil {
			log.Panicln(ctx, "[ETHServiceInstance]: Panic, Error ETHBlockDecimalNumber, err: ", err)
		}
		eTHServiceInstance.recentBlockNumer = dec

		go func() {
			// query eth block number per second.
			// if new block number appear, getBlockByNumber.
			// parse tx into inbount/outbound.
			for range time.Tick(1 * time.Second) {
				if err := eTHServiceInstance.load(ctx); err != nil {
					log.Println(ctx, "[ETHServiceInstance]: eTHServiceInstance load err: ", err)
				}
			}
		}()
	})

	return eTHServiceInstance
}

// GetCurrentBlock get current block.
func (s *ETHService) GetCurrentBlock(ctx context.Context) (*model.ETHBlockInfo, error) {
	num, err := remote.ETHRPCServiceInstance().EthBlockNumber(ctx)
	if err != nil {
		log.Println(ctx, "[GetCurrentBlock]: Error EthBlockNumber, err: ", err)
		return nil, err
	}
	blockInfo, err := remote.ETHRPCServiceInstance().EthGetBlockByNumber(ctx, num)
	if err != nil {
		log.Println(ctx, "[GetCurrentBlock]: Error EthGetBlockByNumber, err: ", err)
		return nil, err
	}
	return blockInfo, nil
}

// Subscribe subscribe an address's inbound/outbound transaction.
func (s *ETHService) Subscribe(ctx context.Context, address string) error {
	address = strings.ToLower(address)
	s.addrRWMutex.Lock()
	s.subAddrs[address] = true
	s.addrRWMutex.Unlock()
	return nil
}

// GetTransactions get address's inbound/outbound transactions
func (s *ETHService) GetTransactions(ctx context.Context, address string) ([]*model.ETHTransaction, error) {
	address = strings.ToLower(address)
	s.txRWMutex.RLock()
	transactions, ok := s.transactions[address]
	if !ok {
		transactions = make([]*model.ETHTransaction, 0)
	}
	s.txRWMutex.RUnlock()
	return transactions, nil
}

// load load transactions via address.
func (s *ETHService) load(ctx context.Context) error {
	// 1. query new block number.
	num, err := remote.ETHRPCServiceInstance().ETHBlockDecimalNumber(ctx)
	if err != nil {
		log.Println(ctx, "[load]: Error EthBlockNumber request:", err)
		return err
	}
	// 2. compare, if no new block, return
	if s.recentBlockNumer >= num {
		// no new block, return.
		return nil
	}
	// 3. update block number.
	s.recentBlockNumer = num
	log.Println(ctx, "[ETHService]: Block Number:", num)
	// 4. parse block transactions.
	err = s.ParseTransactions(ctx, num)
	if err != nil {
		log.Println(ctx, "[load]: Error ParseTransactions request:", err)
		return err
	}
	return nil
}

// ParseTransactions parse block transactions.
func (s *ETHService) ParseTransactions(ctx context.Context, number int64) error {
	hexStr := fmt.Sprintf("0x%x", number)
	blockInfo, err := remote.ETHRPCServiceInstance().EthGetBlockByNumber(ctx, hexStr)
	if err != nil {
		log.Println(ctx, "[ParseTransactions]: Error EthGetBlockByNumber request:", err)
		return err
	}
	transactions := blockInfo.Transactions
	for _, tx := range transactions {
		// if a key exists in map, store it.
		s.addrRWMutex.RLock()
		s.txRWMutex.Lock()
		if _, ok := s.subAddrs[tx.From]; ok {
			// outboundTx: From -> To
			if txList, okk := s.transactions[tx.From]; okk {
				txList = append(txList, tx)
				s.transactions[tx.From] = txList
			} else {
				s.transactions[tx.From] = []*model.ETHTransaction{tx}
			}
		}
		if _, ok := s.subAddrs[tx.To]; ok {
			// inboundTx: From -> To
			if txList, okk := s.transactions[tx.To]; okk {
				txList = append(txList, tx)
				s.transactions[tx.To] = txList
			} else {
				s.transactions[tx.To] = []*model.ETHTransaction{tx}
			}
		}
		s.addrRWMutex.RUnlock()
		s.txRWMutex.Unlock()
	}
	return nil
}