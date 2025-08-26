package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/falconry9527/EasySwapBase/chain"
	"github.com/falconry9527/EasySwapBase/chain/chainclient"
	"github.com/falconry9527/EasySwapBase/ordermanager"
	"github.com/falconry9527/EasySwapBase/stores/xkv"
	"github.com/pkg/errors"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"gorm.io/gorm"

	"github.com/falconry9527/EasySwapSync/service/orderbookindexer"

	"github.com/falconry9527/EasySwapSync/model"
	"github.com/falconry9527/EasySwapSync/service/collectionfilter"
	"github.com/falconry9527/EasySwapSync/service/config"
)

type Service struct {
	ctx              context.Context
	config           *config.Config // 全局配置文件
	kvStore          *xkv.Store     // redis 连接
	db               *gorm.DB       // mysql 连接
	wg               *sync.WaitGroup
	collectionFilter *collectionfilter.Filter   // 筛选器
	orderbookIndexer *orderbookindexer.Service  //  数据解析服务
	orderManager     *ordermanager.OrderManager //
}

func New(ctx context.Context, cfg *config.Config) (*Service, error) {
	var kvConf kv.KvConf
	for _, con := range cfg.Kv.Redis {
		kvConf = append(kvConf, cache.NodeConf{
			RedisConf: redis.RedisConf{
				Host: con.Host,
				Type: con.Type,
				Pass: con.Pass,
			},
			Weight: 2,
		})
	}

	kvStore := xkv.NewStore(kvConf)

	var err error
	db := model.NewDB(cfg.DB)
	// 参数对应的值 : sepolia，OrderBookDex
	collectionFilter := collectionfilter.New(ctx, db, cfg.ChainCfg.Name, cfg.ProjectCfg.Name)
	// 参数对应的值 : sepolia，OrderBookDex
	orderManager := ordermanager.New(ctx, db, kvStore, cfg.ChainCfg.Name, cfg.ProjectCfg.Name)
	var orderbookSyncer *orderbookindexer.Service
	var chainClient chainclient.ChainClient
	fmt.Println("chainClient url:" + cfg.AnkrCfg.HttpsUrl + cfg.AnkrCfg.ApiKey)
	// 只能抓取 允许链id 的数据
	// chain.EthChainID, chain.OptimismChainID, chain.SepoliaChainID
	// chainclient.New 只允许创建 上面 chainId 的客户端
	// 创建eth链的客户端，后面具体请求回用到 chainClient
	chainClient, err = chainclient.New(int(cfg.ChainCfg.ID), cfg.AnkrCfg.HttpsUrl+cfg.AnkrCfg.ApiKey)
	fmt.Printf("-------------")

	if err != nil {
		return nil, errors.Wrap(err, "failed on create evm client")
	}

	switch cfg.ChainCfg.ID {
	case chain.EthChainID, chain.OptimismChainID, chain.SepoliaChainID:
		//参数对应的值 11155111 sepolia
		orderbookSyncer = orderbookindexer.New(ctx, cfg, db, kvStore, chainClient, cfg.ChainCfg.ID, cfg.ChainCfg.Name, orderManager)
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed on create trade info server")
	}
	manager := Service{
		ctx:              ctx,
		config:           cfg,
		db:               db,
		kvStore:          kvStore,
		collectionFilter: collectionFilter,
		orderbookIndexer: orderbookSyncer,
		orderManager:     orderManager,
		wg:               &sync.WaitGroup{},
	}
	return &manager, nil
}

func (s *Service) Start() error {
	// 不要移动位置
	// 查询 ob_collection_ sepolia 标中有的数据，并把合约地址存入集合 Filter.set
	if err := s.collectionFilter.PreloadCollections(); err != nil {
		return errors.Wrap(err, "failed on preload collection to filter")
	}
	// chainclient 同步数据，并把数据存入 数据，发送到 orderManager
	s.orderbookIndexer.Start()
	s.orderManager.Start()
	return nil
}
