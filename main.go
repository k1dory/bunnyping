// bunnyping — TCP-пинг с анимированным зайцем.
//
//	пинг есть  → заяц прыгает и грызёт морковку (зелёная карточка)
//	пинга нет  → заяц спит (zZz, красная карточка)
//
// Примеры:
//
//	bunnyping 192.168.0.42:443
//	bunnyping -host 8.8.8.8 -port 53 -count 5 -delay 1
//	bunnyping 1.1.1.1            (бесконечно, задержка 3с)
//	bunnyping                   (интерактивный ввод)
package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	reset = "\033[0m"
	bold  = "\033[1m"
	// Фон + контрастный текст. Текст рисуется цветом из константы фона, поэтому
	// вся карточка монолитна: чёрным по зелёному / белым по красному.
	greenBG = "\033[48;2;0;170;70m\033[38;2;0;0;0m"
	redBG   = "\033[48;2;200;45;55m\033[38;2;255;255;255m"
	margin  = 4 // отступ слева/справа внутри карточки

	animInterval = 180 * time.Millisecond // период смены кадра анимации
)

// FIGlet-баннеры статуса (шрифт ANSI Shadow) — заголовок над зайцем.
const bannerOpen = `
 ██████╗ ███╗   ██╗██╗     ██╗███╗   ██╗███████╗
██╔═══██╗████╗  ██║██║     ██║████╗  ██║██╔════╝
██║   ██║██╔██╗ ██║██║     ██║██╔██╗ ██║█████╗
██║   ██║██║╚██╗██║██║     ██║██║╚██╗██║██╔══╝
╚██████╔╝██║ ╚████║███████╗██║██║ ╚████║███████╗
 ╚═════╝ ╚═╝  ╚═══╝╚══════╝╚═╝╚═╝  ╚═══╝╚══════╝`

const bannerClosed = `
 ██████╗ ███████╗███████╗██╗     ██╗███╗   ██╗███████╗
██╔═══██╗██╔════╝██╔════╝██║     ██║████╗  ██║██╔════╝
██║   ██║█████╗  █████╗  ██║     ██║██╔██╗ ██║█████╗
██║   ██║██╔══╝  ██╔══╝  ██║     ██║██║╚██╗██║██╔══╝
╚██████╔╝██║     ██║     ███████╗██║██║ ╚████║███████╗
 ╚═════╝ ╚═╝     ╚═╝     ╚══════╝╚═╝╚═╝  ╚═══╝╚══════╝`

// Высота кадра зайца фиксирована (bunnyH строк) — заяц «прыгает» вертикальным
// сдвигом внутри неё, поэтому карточка не дёргается.
const bunnyH = 5

// Прыжок с морковкой: подъём → пик → спуск → приземление → пара кадров жевания.
// Морковка ]==> ]=> ]> * постепенно «съедается».
var activeFrames = [][]string{
	{``, ``, `(\_/)`, `(o.o) ]==>`, `(")_(")`},
	{``, `(\_/)`, `(o.o) ]=>`, `(")_(")`, ``},
	{`(\_/)`, `(^.^) ]>`, `(")_(")`, ``, ``},
	{`(\_/)`, `(^.^) ]`, `(")_(")`, ``, ``},
	{``, `(\_/)`, `(o.o)  *`, `(")_(")`, ``},
	{``, ``, `(\_/)`, `(-.-) ]==>`, `(")_(")`},
	{``, ``, `(\_/)`, `(o.o) ]==>`, `(")_(")`},
	{``, ``, `(\_/)`, `(o.o) ]=>`, `(")_(")`},
}

// Сон: заяц лежит внизу, прикрытые глаза, вверх дрейфуют zZz + кадр-пауза (вдох).
var sleepFrames = [][]string{
	{``, `      z`, `(\_/)`, `(-.-)`, `(")_(")`},
	{``, `       Z`, `(\_/)`, `(-.-)`, `(")_(")`},
	{`      z`, `       Z`, `(\_/)`, `(u.u)`, `(")_(")`},
	{`       Z`, `      z`, `(\_/)`, `(-.-)`, `(")_(")`},
	{``, `      z`, `(\_/)`, `(-.-)`, `(")_(")`},
	{``, ``, `(\_/)`, `(-.-)`, `(")_(")`},
}

const (
	wordActive = "HOP HOP! ням-ням морковку"
	wordSleep  = "zZz... спит, морковки нет"
)

type config struct {
	host    string
	port    int
	count   int           // 0 = бесконечно
	maxTime time.Duration // 0 = без ограничения по времени
	delay   time.Duration
}

func main() {
	fHost := flag.String("host", "", "IP/хост (можно в виде host:port)")
	fPort := flag.Int("port", 0, "TCP-порт (по умолчанию 80)")
	fCount := flag.Int("count", 0, "сколько раз проверять (0 = бесконечно)")
	fTime := flag.Int("time", 0, "ограничение по времени в секундах (0 = без ограничения)")
	fDelay := flag.Float64("delay", 3, "задержка между проверками в секундах")

	// Стандартный flag прекращает разбор на первом позиционном аргументе,
	// поэтому сами вытаскиваем хост и отдаём остальное в flag — тогда порядок
	// "bunnyping 1.2.3.4 -count 5" работает так же, как с флагами впереди.
	positional, rest := extractHost(os.Args[1:])
	flag.CommandLine.Parse(rest)

	cfg := config{
		count:   *fCount,
		maxTime: time.Duration(*fTime) * time.Second,
		delay:   time.Duration(*fDelay * float64(time.Second)),
	}

	host := *fHost
	if host == "" {
		host = positional
	}

	if host == "" {
		interactive(&cfg) // ничего не передали — спрашиваем интерактивно
	} else {
		cfg.host, cfg.port = splitHostPort(host, *fPort)
	}

	if cfg.port == 0 {
		cfg.port = 80
	}
	if cfg.delay <= 0 {
		cfg.delay = 3 * time.Second
	}

	enableVT() // ANSI-цвета в консоли Windows (на остальных ОС — no-op)
	run(cfg)
}

// extractHost вытаскивает первый позиционный аргумент (хост) из argv,
// возвращая его и остаток без него — остаток скармливается flag.Parse.
func extractHost(args []string) (host string, rest []string) {
	valueFlag := map[string]bool{"host": true, "port": true, "count": true, "time": true, "delay": true}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			rest = append(rest, a)
			name := strings.TrimLeft(a, "-")
			if !strings.Contains(a, "=") && valueFlag[name] && i+1 < len(args) {
				rest = append(rest, args[i+1])
				i++
			}
			continue
		}
		if host == "" {
			host = a // первый «голый» токен — это хост
		} else {
			rest = append(rest, a)
		}
	}
	return host, rest
}

// splitHostPort разбирает "host:port"; флаг -port имеет приоритет.
func splitHostPort(s string, flagPort int) (string, int) {
	if strings.Count(s, ":") == 1 {
		h, p, _ := strings.Cut(s, ":")
		if flagPort == 0 {
			if n, err := strconv.Atoi(p); err == nil {
				return h, n
			}
		}
		return h, flagPort
	}
	return s, flagPort
}

func interactive(cfg *config) {
	in := bufio.NewReader(os.Stdin)

	for cfg.host == "" {
		raw := prompt(in, "IP/хост (можно host:port): ")
		if raw == "" {
			continue
		}
		cfg.host, cfg.port = splitHostPort(raw, 0)
	}

	if cfg.port == 0 {
		if v := prompt(in, "Порт [80]: "); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				cfg.port = n
			}
		}
	}

	if v := prompt(in, "Сколько раз проверять (пусто = бесконечно): "); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.count = n
		}
	}

	if v := prompt(in, "Ограничение по времени, сек (пусто = без ограничения): "); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.maxTime = time.Duration(n) * time.Second
		}
	}

	if v := prompt(in, "Задержка между проверками, сек [3]: "); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			cfg.delay = time.Duration(f * float64(time.Second))
		}
	}
}

func prompt(in *bufio.Reader, label string) string {
	fmt.Print(label)
	line, _ := in.ReadString('\n')
	line = strings.TrimPrefix(line, "\uFEFF") // BOM от некоторых терминалов/пайпов
	return strings.TrimSpace(line)
}

type probeResult struct {
	ok   bool
	info string
}

func run(cfg config) {
	target := net.JoinHostPort(cfg.host, strconv.Itoa(cfg.port))
	cardW := cardWidth(target)

	// Одиночная проверка — рисуем один статичный кадр без анимации/курсора.
	if cfg.count == 1 {
		ok, info := probe(cfg)
		paint(target, ok, info, 0, cardW, false)
		if !ok {
			os.Exit(1)
		}
		return
	}

	fmt.Print("\033[2J\033[?25l") // очистка экрана + скрыть курсор
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		<-sig
		fmt.Print(reset + "\033[?25h\nЗайчик убежал. Пока!\n")
		os.Exit(130)
	}()

	results := make(chan probeResult, 1)
	go prober(cfg, results)

	ok, info, lastOK := false, "соединяюсь...", false
	tick := time.NewTicker(animInterval)
	defer tick.Stop()

	for frame := 0; ; frame++ {
		select {
		case r, more := <-results:
			if !more { // пробер закончил (исчерпан count или время)
				fmt.Print("\033[?25h")
				if !lastOK {
					os.Exit(1)
				}
				return
			}
			ok, info, lastOK = r.ok, r.info, r.ok
		case <-tick.C:
			paint(target, ok, info, frame, cardW, true)
		}
	}
}

// prober в фоне пингует с интервалом cfg.delay и шлёт результаты в канал,
// чтобы анимация в основном цикле не зависела от таймаутов соединения.
func prober(cfg config, out chan<- probeResult) {
	start := time.Now()
	for i := 1; ; i++ {
		ok, info := probe(cfg)
		out <- probeResult{ok, info}
		if cfg.count > 0 && i >= cfg.count {
			break
		}
		if cfg.maxTime > 0 && time.Since(start) >= cfg.maxTime {
			break
		}
		time.Sleep(cfg.delay)
		if cfg.maxTime > 0 && time.Since(start) >= cfg.maxTime {
			break
		}
	}
	close(out)
}

func probe(cfg config) (bool, string) {
	addr := net.JoinHostPort(cfg.host, strconv.Itoa(cfg.port))
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, cfg.delay)
	if err != nil {
		return false, shortErr(err)
	}
	_ = conn.Close()
	return true, fmt.Sprintf("%dms", time.Since(start).Milliseconds())
}

func shortErr(err error) string {
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return "timeout"
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "refused"):
		return "connection refused"
	case strings.Contains(s, "no such host"):
		return "не резолвится"
	default:
		return "нет соединения"
	}
}

// line — строка содержимого карточки. centered=true → центрируется внутри
// карточки; иначе выравнивается по левому краю (так баннер сохраняет внутреннюю
// раскладку: центрировать его строки по отдельности нельзя — разъедутся).
type line struct {
	text     string
	centered bool
}

func paint(target string, ok bool, info string, frame, cardW int, redraw bool) {
	bg, banner, frames, word := redBG, bannerClosed, sleepFrames, wordSleep
	if ok {
		bg, banner, frames, word = greenBG, bannerOpen, activeFrames, wordActive
	}

	status := "TCP " + target + "  •  " + statusWord(ok)
	if info != "" {
		status += "  (" + info + ")"
	}

	// Карточка: баннер (блоком) → заяц → тема → статус, с пустыми строками-отступами.
	var content []line
	content = append(content, line{"", false})
	for _, ln := range bannerLines(banner) {
		content = append(content, line{ln, false})
	}
	content = append(content, line{"", false})
	for _, ln := range frames[frame%len(frames)] {
		content = append(content, line{ln, true})
	}
	content = append(content, line{"", false}, line{word, true}, line{"", false}, line{status, true}, line{"", false})

	inner := cardW - margin*2 // внутренняя ширина без боковых отступов
	var b strings.Builder
	if redraw {
		b.WriteString("\033[H") // курсор в угол — перерисовываем поверх старого кадра
	}
	for _, ln := range content {
		n := utf8.RuneCountInString(ln.text)
		left := 0
		if ln.centered {
			left = (inner - n) / 2
		}
		right := max(inner-left-n, 0)
		b.WriteString(bg + bold + strings.Repeat(" ", margin+left) + ln.text + strings.Repeat(" ", right) + reset)
		b.WriteString("\033[K\n") // гасим хвост строки от прошлого кадра
	}
	fmt.Print(b.String())
}

// bannerLines режет FIGlet-баннер на строки (убирая обрамляющие переносы).
func bannerLines(s string) []string {
	return strings.Split(strings.Trim(s, "\n"), "\n")
}

func statusWord(ok bool) string {
	if ok {
		return "ONLINE"
	}
	return "OFFLINE"
}

// cardWidth считает фиксированную ширину карточки заранее — по всем кадрам и
// возможным статусным строкам, чтобы ширина не «дышала» от кадра к кадру.
func cardWidth(target string) int {
	var samples []string
	samples = append(samples, bannerLines(bannerOpen)...)
	samples = append(samples, bannerLines(bannerClosed)...)
	for _, fr := range activeFrames {
		samples = append(samples, fr...)
	}
	for _, fr := range sleepFrames {
		samples = append(samples, fr...)
	}
	samples = append(samples, wordActive, wordSleep)
	infos := []string{"соединяюсь...", "connection refused", "не резолвится", "нет соединения", "timeout", "9999ms"}
	for _, w := range []string{"ONLINE", "OFFLINE"} {
		for _, inf := range infos {
			samples = append(samples, "TCP "+target+"  •  "+w+"  ("+inf+")")
		}
	}

	max := 0
	for _, s := range samples {
		if n := utf8.RuneCountInString(s); n > max {
			max = n
		}
	}
	return max + margin*2
}
