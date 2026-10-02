package server
// 
// import (
//   "fmt"
//   "strings"
//   "net/http"
//   "io/ioutil"
// )
// 
// func main() {
// 
//   url := "http://localhost:34959/v1/chat/completions"
//   method := "POST"
// 
//   payload := strings.NewReader(`{
//   "messages": [
//     {
//       "content": "You are a helpful assistant.",
//       "role": "system"
//     },
//     {
//       "content": "What is the capital of France?",
//       "role": "user"
//     }
//   ]
// }`)
// 
//   client := &http.Client {
//   }
//   req, err := http.NewRequest(method, url, payload)
// 
//   if err != nil {
//     fmt.Println(err)
//     return
//   }
//   req.Header.Add("Content-Type", "application/json")
// 
//   res, err := client.Do(req)
//   if err != nil {
//     fmt.Println(err)
//     return
//   }
//   defer res.Body.Close()
// 
//   body, err := ioutil.ReadAll(res.Body)
//   if err != nil {
//     fmt.Println(err)
//     return
//   }
//   fmt.Println(string(body))
// }