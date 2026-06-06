package memorycore

import appcore "github.com/longyisang/emoagent-memorycore/internal/app/memorycore"

type QueryAnalysisCache struct {
	inner *appcore.QueryAnalysisCache
}

func NewQueryAnalysisCache() *QueryAnalysisCache {
	return &QueryAnalysisCache{inner: appcore.NewQueryAnalysisCache()}
}

func (c *QueryAnalysisCache) appCache() *appcore.QueryAnalysisCache {
	if c == nil {
		return nil
	}
	if c.inner == nil {
		c.inner = appcore.NewQueryAnalysisCache()
	}
	return c.inner
}
