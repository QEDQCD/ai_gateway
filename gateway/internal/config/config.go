package config

type Config struct {
	ListenAddr string
}

func Load() Config {
	return Config{
		ListenAddr: ":8080",
	}
}
