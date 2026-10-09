package douyin

import (
	"net/url"
	"testing"
)

func TestPreferHTTPSStreamURL(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"抖音HTTP流", "http://pull-flv-l1.douyincdn.com/third/live.flv?wsSecret=a%2Fb&wsTime=123", "https://pull-flv-l1.douyincdn.com/third/live.flv?wsSecret=a%2Fb&wsTime=123"},
		{"默认端口", "http://pull-flv-l1.douyincdn.com:80/live.flv", "https://pull-flv-l1.douyincdn.com/live.flv"},
		{"HTTPS不变", "https://pull-flv-l1.douyincdn.com/live.flv", "https://pull-flv-l1.douyincdn.com/live.flv"},
		{"其他域名不变", "http://example.com/live.flv", "http://example.com/live.flv"},
		{"伪装后缀不变", "http://douyincdn.com.example.com/live.flv", "http://douyincdn.com.example.com/live.flv"},
		{"伪装前缀不变", "http://evildouyincdn.com/live.flv", "http://evildouyincdn.com/live.flv"},
		{"自定义端口不变", "http://pull-flv-l1.douyincdn.com:8080/live.flv", "http://pull-flv-l1.douyincdn.com:8080/live.flv"},
		{"IPv6地址不变", "http://[240e:f7:8e00:1003::5a]/live.flv", "http://[240e:f7:8e00:1003::5a]/live.flv"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := url.Parse(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			got := preferHTTPSStreamURL(u)
			if got.String() != tc.want {
				t.Fatalf("流地址不符合预期: got %q, want %q", got.String(), tc.want)
			}
			if u.String() != tc.input {
				t.Fatal("原流地址不应被修改")
			}
		})
	}
	if preferHTTPSStreamURL(nil) != nil {
		t.Fatal("空地址应保持为空")
	}
}
