package service

// builtinRuleSet is a rule set shipped with the panel. Bump Version when Rules change so panels
// whose admin never edited the set pick up the new list on upgrade.
type builtinRuleSet struct {
	Key, Name, Category, Policy string
	Version                     int
	Rules                       []string
}

// Built-in names are Chinese because the catalog targets mainland users; admins can rename them.
var builtinRuleSets = []builtinRuleSet{
	{Key: "cn-banks", Name: "国内银行与支付", Category: "银行", Policy: "DIRECT", Version: 1, Rules: []string{
		"icbc.com.cn", "ccb.com", "abchina.com", "abchina.com.cn", "boc.cn", "bankcomm.com", "cmbchina.com",
		"cmbc.com.cn", "citicbank.com", "cib.com.cn", "spdb.com.cn", "cebbank.com", "hxb.com.cn",
		"pingan.com", "pingan.com.cn", "psbc.com", "cgbchina.com.cn", "bankofbeijing.com.cn", "bosc.cn",
		"njcb.com.cn", "hzbank.com.cn", "czbank.com", "webank.com", "mybank.cn", "unionpay.com",
		"95516.com", "alipay.com", "alipayobjects.com", "tenpay.com", "chinapay.com", "bankofchina.com",
	}},
	{Key: "intl-banks", Name: "境外银行与支付", Category: "银行", Policy: "PROXY", Version: 1, Rules: []string{
		"hsbc.com", "hsbc.com.hk", "hsbc.co.uk", "hangseng.com", "bochk.com", "sc.com", "citibank.com",
		"citi.com", "citibank.com.hk", "jpmorgan.com", "chase.com", "bankofamerica.com", "wellsfargo.com",
		"dbs.com", "dbs.com.sg", "dbs.com.hk", "ocbc.com", "uob.com.sg", "mufg.jp", "smbc.co.jp",
		"barclays.co.uk", "db.com", "bnpparibas.com", "ubs.com", "credit-suisse.com", "paypal.com",
		"paypalobjects.com", "wise.com", "stripe.com", "payoneer.com", "airwallex.com", "revolut.com",
		"swift.com", "westernunion.com",
	}},
	{Key: "intl-express", Name: "国际快递", Category: "物流", Policy: "PROXY", Version: 1, Rules: []string{
		"dhl.com", "dhl.de", "dhl.com.hk", "fedex.com", "ups.com", "tnt.com", "usps.com", "aramex.com",
		"royalmail.com", "canadapost-postescanada.ca", "auspost.com.au", "japanpost.jp", "parcelsapp.com",
		"aftership.com",
	}},
	{Key: "cn-logistics", Name: "国内物流快递", Category: "物流", Policy: "DIRECT", Version: 1, Rules: []string{
		"sf-express.com", "sf-international.com", "jdl.com", "zto.com", "yto.net.cn", "sto.cn",
		"yundaex.com", "ems.com.cn", "11183.com.cn", "deppon.com", "kuaidi100.com", "cainiao.com",
		"jtexpress.cn", "17track.net", "best-inc.com", "ane56.com", "kye-erp.com",
	}},
	{Key: "shipping-lines", Name: "国际船公司", Category: "船公司", Policy: "PROXY", Version: 1, Rules: []string{
		"maersk.com", "maerskline.com", "sealandmaersk.com", "msc.com", "cma-cgm.com", "apl.com",
		"anl.com.au", "hapag-lloyd.com", "one-line.com", "evergreen-line.com", "shipmentlink.com",
		"yangming.com", "hmm21.com", "zim.com", "pilship.com", "wanhai.com", "oocl.com", "kmtc.co.kr",
		"sinokor.co.kr", "hamburgsud.com", "matson.com", "crowley.com", "tslines.com", "emiratesline.com",
	}},
	{Key: "cn-shipping", Name: "国内船公司与港口口岸", Category: "船公司", Policy: "DIRECT", Version: 1, Rules: []string{
		"coscoshipping.com", "sinotrans.com", "sitc.com", "portshanghai.com.cn", "nbport.com.cn",
		"singlewindow.cn", "customs.gov.cn", "chinaport.gov.cn", "sipg.com.cn", "cmport.com.hk",
		"cosco.com", "chinashipping.com.cn",
	}},
	{Key: "freight-tracking", Name: "货运追踪与订舱平台", Category: "物流", Policy: "PROXY", Version: 1, Rules: []string{
		"inttra.com", "cargosmart.com", "searates.com", "marinetraffic.com", "vesselfinder.com",
		"freightos.com", "flexport.com", "gocomet.com", "portcast.io", "track-trace.com", "shipsgo.com",
		"container-tracking.org",
	}},
	{Key: "cross-border", Name: "跨境电商平台", Category: "外贸", Policy: "PROXY", Version: 1, Rules: []string{
		"amazon.com", "amazon.co.uk", "amazon.de", "amazon.co.jp", "amazon.ca", "amazon.com.au",
		"sellercentral.amazon.com", "media-amazon.com", "ssl-images-amazon.com", "ebay.com", "ebayimg.com",
		"shopify.com", "myshopify.com", "shopifycdn.com", "etsy.com", "etsystatic.com", "walmart.com",
		"mercadolibre.com", "lazada.com", "shopee.com", "tiktokshop.com",
	}},
	{Key: "teams", Name: "Microsoft Teams 会议", Category: "办公", Policy: "PROXY", Version: 1, Rules: []string{
		"teams.microsoft.com", "teams.live.com", "teams.cdn.office.net", "skype.com", "lync.com",
		"skypeforbusiness.com",
	}},
	{Key: "ai", Name: "AI 服务", Category: "办公", Policy: "PROXY", Version: 1, Rules: []string{
		"openai.com", "chatgpt.com", "oaistatic.com", "oaiusercontent.com", "anthropic.com", "claude.ai",
		"claudeusercontent.com", "gemini.google.com", "generativelanguage.googleapis.com", "perplexity.ai",
		"poe.com", "x.ai", "grok.com", "copilot.microsoft.com", "midjourney.com",
	}},
}
