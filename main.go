package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(10 * time.Second))
	r.Use(middleware.NoCache)

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		prices := fetchPriceData()
		l, _ := time.LoadLocation("Europe/Copenhagen")

		frontendPrices := make([]FrontendHourlyPrice, 0)

		for _, hour := range prices {
			d, err := time.Parse("2006-01-02T15:04:05", hour.HourUTC)
			if err != nil {
				panic(err)
			}

			//The raw price (ex. taxes and fees, from Nordpool)
			pricePrKwh := hour.SpotPriceDKK / 1000

			//See: https://cerius.dk/priser-og-tariffer/tariffer-og-netabonnement/
			//All our fees are stored in kr/kWh (Cerius publishes in øre/kwh)
			//"elafgift" is always the same
			stateTariff := 0.7610
			pricePrKwh += stateTariff

			//EWII "Grøn indkøbspris" (3.75 øre pr. kWh fixed for hourly fees, 6 øre pr. kWh for green power)
			ewiiFees := 0.0975
			pricePrKwh += ewiiFees

			//Summer/winter tariffs (low, high, peak)
			winterTariffs := []float64{0.1126, 0.3378, 1.0133}
			summerTariffs := []float64{0.1126, 0.1689, 0.4391}

			//Check if we are in summer/winter tariff time.
			currentMonth := int(d.Month())
			//If between April and September, it's summer timer!
			var tariffs []float64
			if currentMonth >= 4 && currentMonth <= 9 {
				tariffs = summerTariffs
			} else {
				tariffs = winterTariffs
			}

			//Check which tariff we are at based on hourly schedule.
			//The schedule is the same year round, but we have to change out the prices summer/winter.
			currentHour := int(d.Hour())
			if currentHour >= 0 && currentHour < 6 {
				pricePrKwh += tariffs[0]
			}
			if currentHour >= 6 && currentHour < 17 {
				pricePrKwh += tariffs[1]
			}
			if currentHour >= 17 && currentHour < 21 {
				pricePrKwh += tariffs[2]
			}
			if currentHour >= 21 && currentHour <= 24 {
				pricePrKwh += tariffs[1]
			}

			//Finally apply 25% VAT
			pricePrKwh *= 1.25

			frontendPrices = append(frontendPrices, FrontendHourlyPrice{d.In(l).Format(time.RFC3339), fmt.Sprintf("%f", pricePrKwh)})
		}
		slices.Reverse(frontendPrices)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK) // adding a status 200
		json.NewEncoder(w).Encode(FrontendResponse{frontendPrices})
	})

	fmt.Println("Starting spotprice server!")
	http.ListenAndServe(":8080", r)
}

type FrontendHourlyPrice struct {
	StarTime string `json:"start_time"`
	Price    string `json:"price"`
}
type FrontendResponse struct {
	SpotPrices []FrontendHourlyPrice `json:"spot_prices"`
}
type HourlyPrice struct {
	HourUTC      string
	HourDK       string
	PriceArea    string
	SpotPriceDKK float64
	SpotPriceEUR float64
}
type PriceDataResponse struct {
	Total   int
	Filters string
	Limit   int
	Dataset string
	Records []HourlyPrice
}

func fetchPriceData() []HourlyPrice {
	resp, err := http.Get("https://api.energidataservice.dk/dataset/Elspotprices?limit=168&filter={%22PriceArea%22:%22DK2%22}")
	if err != nil {
		log.Fatalln(err)
	}
	//We Read the response body on the line below.
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatalln(err)
	}
	//Convert the body to type string
	sb := string(body)

	var priceDataResponse PriceDataResponse
	json.Unmarshal([]byte(sb), &priceDataResponse)

	return priceDataResponse.Records
}
