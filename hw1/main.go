package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

var prepositionalCases = map[string]string{
	"стол": "на столе",
	"стул": "на стуле",
}

type Furniture struct {
	Name  string
	Items []string
}

func NewFurniture(name string, items ...string) *Furniture {
	return &Furniture{
		Name:  name,
		Items: items,
	}
}

func (f *Furniture) RemoveItem(item string) bool {
	for i, it := range f.Items {
		if it == item {
			f.Items = append(f.Items[:i], f.Items[i+1:]...)
			return true
		}
	}
	return false
}

type Door struct {
	Name     string
	KeyItem  string
	IsLocked bool
}

type Exit struct {
	Name string
	Room *Room
	Door *Door
}

type Room struct {
	Name           string
	EntryText      string
	Furnitures     []*Furniture
	Exits          []*Exit
	LookAroundFunc func(r *Room, p *Player) string
}

func NewRoom(name, entryText string) *Room {
	return &Room{
		Name:      name,
		EntryText: entryText,
	}
}

func (r *Room) AddFurniture(furniture *Furniture) {
	r.Furnitures = append(r.Furnitures, furniture)
}

func (r *Room) AddExit(name string, target *Room, door *Door) {
	r.Exits = append(r.Exits, &Exit{
		Name: name,
		Room: target,
		Door: door,
	})
}

func (r *Room) FindExit(name string) *Exit {
	for _, exit := range r.Exits {
		if exit.Name == name {
			return exit
		}
	}
	return nil
}

func (r *Room) FindDoor(name string) *Door {
	for _, exit := range r.Exits {
		if exit.Door != nil && exit.Door.Name == name {
			return exit.Door
		}
	}
	return nil
}

func (r *Room) RemoveItem(item string) bool {
	for _, f := range r.Furnitures {
		if f.RemoveItem(item) {
			return true
		}
	}
	return false
}

func (r *Room) FormatExits() string {
	exitNames := make([]string, 0, len(r.Exits))
	for _, exit := range r.Exits {
		exitNames = append(exitNames, exit.Name)
	}
	return strings.Join(exitNames, ", ")
}

func (r *Room) FormatItems() string {
	var parts []string
	for _, f := range r.Furnitures {
		if len(f.Items) > 0 {
			prep, ok := prepositionalCases[f.Name]
			if !ok {
				prep = "на " + f.Name
			}
			parts = append(parts, fmt.Sprintf("%s: %s", prep, strings.Join(f.Items, ", ")))
		}
	}
	return strings.Join(parts, ", ")
}

func (r *Room) EntryDescription() string {
	return fmt.Sprintf("%s. можно пройти - %s", r.EntryText, r.FormatExits())
}

func (r *Room) LookAround(p *Player) string {
	if r.LookAroundFunc != nil {
		return r.LookAroundFunc(r, p)
	}

	itemsInfo := r.FormatItems()
	if itemsInfo == "" {
		itemsInfo = "пустая комната"
	}

	return fmt.Sprintf("%s. можно пройти - %s", itemsInfo, r.FormatExits())
}

type Player struct {
	CurrentRoom *Room
	HasBackpack bool
	Inventory   []string
}

func NewPlayer(currentRoom *Room, hasBackpack bool) *Player {
	return &Player{
		CurrentRoom: currentRoom,
		HasBackpack: hasBackpack,
		Inventory:   make([]string, 0),
	}
}

func (p *Player) HasItem(item string) bool {
	for _, it := range p.Inventory {
		if it == item {
			return true
		}
	}
	return false
}

var player *Player

func initGame() {
	kitchen := NewRoom("кухня", "кухня, ничего интересного")
	corridor := NewRoom("коридор", "ничего интересного")
	room := NewRoom("комната", "ты в своей комнате")
	street := NewRoom("улица", "на улице весна")

	kitchenTable := NewFurniture("стол", "чай")
	kitchen.AddFurniture(kitchenTable)

	roomTable := NewFurniture("стол", "ключи", "конспекты")
	roomChair := NewFurniture("стул", "рюкзак")
	room.AddFurniture(roomTable)
	room.AddFurniture(roomChair)

	kitchen.LookAroundFunc = func(r *Room, p *Player) string {
		quest := "надо собрать рюкзак и идти в универ"
		if p.HasBackpack {
			quest = "надо идти в универ"
		}

		itemsInfo := r.FormatItems()
		if itemsInfo != "" {
			return fmt.Sprintf("ты находишься на кухне, %s, %s. можно пройти - %s", itemsInfo, quest, r.FormatExits())
		}
		return fmt.Sprintf("ты находишься на кухне, %s. можно пройти - %s", quest, r.FormatExits())
	}

	doorToStreet := &Door{
		Name:     "дверь",
		KeyItem:  "ключи",
		IsLocked: true,
	}

	kitchen.AddExit("коридор", corridor, nil)

	corridor.AddExit("кухня", kitchen, nil)
	corridor.AddExit("комната", room, nil)
	corridor.AddExit("улица", street, doorToStreet)

	room.AddExit("коридор", corridor, nil)

	street.AddExit("домой", corridor, nil)

	player = NewPlayer(kitchen, false)
}

func handleCommand(command string) string {
	parts := strings.Split(command, " ")
	if len(parts) == 0 || parts[0] == "" {
		return "неизвестная команда"
	}

	cmd := parts[0]

	switch cmd {
	case "осмотреться":
		return player.CurrentRoom.LookAround(player)

	case "идти":
		if len(parts) < 2 {
			return "неизвестная команда"
		}
		target := parts[1]
		exit := player.CurrentRoom.FindExit(target)
		if exit == nil {
			return "нет пути в " + target
		}
		if exit.Door != nil && exit.Door.IsLocked {
			return "дверь закрыта"
		}
		player.CurrentRoom = exit.Room
		return player.CurrentRoom.EntryDescription()

	case "надеть":
		if len(parts) < 2 {
			return "неизвестная команда"
		}
		item := parts[1]
		if item == "рюкзак" && player.CurrentRoom.RemoveItem(item) {
			player.HasBackpack = true
			return "вы надели: " + item
		}
		return "нет такого"

	case "взять":
		if len(parts) < 2 {
			return "неизвестная команда"
		}
		item := parts[1]
		if !player.HasBackpack {
			return "некуда класть"
		}
		if player.CurrentRoom.RemoveItem(item) {
			player.Inventory = append(player.Inventory, item)
			return "предмет добавлен в инвентарь: " + item
		}
		return "нет такого"

	case "применить":
		if len(parts) < 3 {
			return "неизвестная команда"
		}
		item := parts[1]
		target := parts[2]

		if !player.HasItem(item) {
			return "нет предмета в инвентаре - " + item
		}

		door := player.CurrentRoom.FindDoor(target)
		if door != nil && door.KeyItem == item {
			door.IsLocked = false
			return "дверь открыта"
		}

		return "не к чему применить"

	default:
		return "неизвестная команда"
	}
}

func main() {
	initGame()

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		command := strings.TrimSpace(scanner.Text())
		if command == "exit" {
			break
		}
		if command == "" {
			continue
		}

		response := handleCommand(command)
		fmt.Println(response)
	}
}
