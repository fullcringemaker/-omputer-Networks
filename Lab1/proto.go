package proto

type Request struct {
    Number string `json:"number"`
    Digit  string `json:"digit"`
}

type Response struct {
    Status  string `json:"status"`
    Message string `json:"message"`
    Count   int    `json:"count"`
}
